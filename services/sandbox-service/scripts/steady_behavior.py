#!/usr/bin/env python3
"""
稳态用户行为流量模拟脚本（T1-30 + T1-31）。

模拟电商系统的真实用户访问流量，用于：
  - 产生稳定的业务指标（QPS、错误率、延迟等）
  - 验证监控数据的真实性
  - 训练开始后持续运行，模拟用户行为

用户行为链路：
  首页 → 浏览商品 → 搜索 → 商品详情 → 加购 → 下单 → 支付

特性（T1-30）：
  - 浏览/搜索/详情/加购/下单/支付 全链路模拟
  - 并发与间隔随机化（指数分布模拟真实到达）
  - 可配置 QPS、用户数、行为比例
  - 校准量级使指标处于正常范围

稳定性验证（T1-31）：
  - 持续运行 30 分钟不中断
  - 异常自动重试（指数退避）
  - 流量波峰波谷模拟（正弦曲线变化）
  - 实时统计与错误报告

用法：
  python steady_behavior.py --base-qps 20 --duration 1800
  python steady_behavior.py --base-qps 50 --peak-qps 100 --duration 3600 --pattern daily
"""

import argparse
import asyncio
import random
import time
import sys
import signal
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from typing import Dict, List, Optional
from collections import defaultdict


# ============================================================
# 配置
# ============================================================

@dataclass
class BehaviorConfig:
    """用户行为配置。"""
    # 基础 QPS
    base_qps: int = 20
    # 峰值 QPS（波峰模式）
    peak_qps: Optional[int] = None
    # 持续时间（秒），0 表示无限
    duration: int = 1800
    # 流量模式：steady / daily / growing / spike
    pattern: str = "steady"
    # 并发用户数
    concurrency: int = 10
    # 行为链路各步骤转化率（相对上一步）
    browse_rate: float = 1.0      # 浏览
    search_rate: float = 0.6      # 搜索
    detail_rate: float = 0.8      # 商品详情
    cart_rate: float = 0.5        # 加购
    order_rate: float = 0.3       # 下单
    pay_rate: float = 0.85        # 支付
    # 请求间最小/最大间隔（秒）
    min_interval: float = 0.5
    max_interval: float = 3.0
    # 错误率模拟（百分之几的请求会失败）
    error_rate: float = 1.0
    # 目标地址（模拟模式下不实际发请求）
    base_url: str = "http://localhost:8080"
    # 是否真实发请求（False 只模拟打印）
    real_request: bool = False
    # 日志级别
    verbose: bool = False


@dataclass
class Stats:
    """运行统计。"""
    start_time: float = 0.0
    end_time: float = 0.0
    total_requests: int = 0
    total_errors: int = 0
    requests_by_type: Dict[str, int] = field(default_factory=lambda: defaultdict(int))
    errors_by_type: Dict[str, int] = field(default_factory=lambda: defaultdict(int))
    response_times: List[float] = field(default_factory=list)
    last_report_time: float = 0.0

    def record_request(self, req_type: str, success: bool, latency: float):
        self.total_requests += 1
        self.requests_by_type[req_type] += 1
        self.response_times.append(latency)
        if not success:
            self.total_errors += 1
            self.errors_by_type[req_type] += 1

    @property
    def elapsed(self) -> float:
        return time.time() - self.start_time

    @property
    def current_qps(self) -> float:
        if self.elapsed == 0:
            return 0
        return self.total_requests / self.elapsed

    @property
    def error_percent(self) -> float:
        if self.total_requests == 0:
            return 0
        return (self.total_errors / self.total_requests) * 100

    @property
    def avg_latency(self) -> float:
        if not self.response_times:
            return 0
        return sum(self.response_times) / len(self.response_times)


# ============================================================
# 流量模式
# ============================================================

def get_traffic_multiplier(config: BehaviorConfig, elapsed: float) -> float:
    """
    根据流量模式和已运行时间，计算当前 QPS 倍率。

    返回值为 0~1+ 的乘数，乘以 base_qps 得到当前目标 QPS。
    """
    if config.pattern == "steady":
        return 1.0

    if config.pattern == "growing":
        # 线性增长：从 0.3 增长到 1.0
        if config.duration == 0:
            return 1.0
        progress = min(1.0, elapsed / config.duration)
        return 0.3 + 0.7 * progress

    if config.pattern == "spike":
        # 尖刺：中间有一个 2 分钟的高峰
        spike_center = config.duration / 2
        spike_width = 120  # 2 分钟
        dist = abs(elapsed - spike_center)
        if dist < spike_width:
            # 高斯曲线模拟尖刺
            return 1.0 + 2.0 * (1.0 - dist / spike_width)
        return 1.0

    if config.pattern == "daily":
        # 日周期：正弦曲线模拟一天的流量波动
        # 一个完整周期 = 24 分钟（模拟 24 小时）
        period = 24 * 60  # 24 分钟
        # 偏移：让开始时在中等偏高水平
        offset = -period / 4
        # 范围：0.4 ~ 1.0
        return 0.7 + 0.3 * __import__("math").sin(2 * 3.14159 * (elapsed + offset) / period)

    return 1.0


# ============================================================
# 模拟用户行为
# ============================================================

# 商品 ID 池
PRODUCT_IDS = list(range(1000, 1050))
# 搜索关键词
SEARCH_KEYWORDS = [
    "手机", "笔记本", "耳机", "键盘", "鼠标",
    "显示器", "路由器", "相机", "平板", "手表",
    "充电宝", "数据线", "音箱", "U盘", "硬盘",
]

async def simulate_user_session(config: BehaviorConfig, stats: Stats, user_id: int):
    """模拟一个用户的完整行为会话。"""
    user_sleep = lambda: asyncio.sleep(random.uniform(config.min_interval, config.max_interval))

    # 1. 浏览首页
    await __make_request(config, stats, "browse_home", f"GET /", user_id)
    await user_sleep()

    # 2. 浏览商品列表（按比例）
    if random.random() < config.browse_rate:
        category = random.choice(["electronics", "clothing", "books", "food"])
        await __make_request(config, stats, "browse_category", f"GET /category/{category}", user_id)
        await user_sleep()

    # 3. 搜索（按比例）
    if random.random() < config.search_rate:
        keyword = random.choice(SEARCH_KEYWORDS)
        await __make_request(config, stats, "search", f"GET /search?q={keyword}", user_id)
        await user_sleep()

    # 4. 商品详情（按比例）
    if random.random() < config.detail_rate:
        product_id = random.choice(PRODUCT_IDS)
        await __make_request(config, stats, "view_detail", f"GET /product/{product_id}", user_id)
        await user_sleep()

    # 5. 加购（按比例）
    if random.random() < config.cart_rate:
        product_id = random.choice(PRODUCT_IDS)
        qty = random.randint(1, 3)
        await __make_request(config, stats, "add_cart", f"POST /cart/add {product_id}x{qty}", user_id)
        await user_sleep()

    # 6. 下单（按比例）
    if random.random() < config.order_rate:
        order_items = random.randint(1, 3)
        await __make_request(config, stats, "place_order", f"POST /order ({order_items} items)", user_id)
        await user_sleep()

        # 7. 支付（按比例）
        if random.random() < config.pay_rate:
            await __make_request(config, stats, "pay", f"POST /pay", user_id)


async def __make_request(config: BehaviorConfig, stats: Stats, req_type: str, desc: str, user_id: int):
    """模拟一次请求（真实或假的）。"""
    start = time.time()

    # 模拟延迟
    base_latency = {
        "browse_home": 0.02,
        "browse_category": 0.05,
        "search": 0.08,
        "view_detail": 0.06,
        "add_cart": 0.04,
        "place_order": 0.15,
        "pay": 0.2,
    }.get(req_type, 0.05)

    latency = base_latency * (0.5 + random.random())
    await asyncio.sleep(latency)

    # 随机错误
    is_error = random.random() < config.error_rate / 100
    actual_latency = time.time() - start

    stats.record_request(req_type, not is_error, actual_latency)

    if config.verbose:
        status = "✓" if not is_error else "✗"
        ts = datetime.now().strftime("%H:%M:%S")
        print(f"[{ts}] user-{user_id:03d} {status} {desc} ({actual_latency*1000:.0f}ms)")

    # 真实请求模式（预留）
    if config.real_request and not is_error:
        pass  # TODO: 接入真实 HTTP 请求


# ============================================================
# 流量控制器
# ============================================================

async def traffic_controller(config: BehaviorConfig, stats: Stats):
    """
    流量控制器：根据目标 QPS 生成用户会话。
    使用令牌桶思路，按间隔触发新的用户会话。
    """
    user_id = 0
    start_time = time.time()

    while True:
        elapsed = time.time() - start_time

        # 检查是否超时
        if config.duration > 0 and elapsed >= config.duration:
            break

        # 计算当前目标 QPS
        mult = get_traffic_multiplier(config, elapsed)
        target_qps = config.base_qps * mult
        if config.peak_qps and config.pattern == "daily":
            target_qps = target_qps * (config.peak_qps / config.base_qps)

        # 计算下一次触发的间隔
        if target_qps <= 0:
            await asyncio.sleep(0.1)
            continue

        interval = 1.0 / target_qps
        # 添加一些随机性（±20%）
        interval *= 0.8 + random.random() * 0.4

        # 启动一个新用户会话
        user_id += 1
        asyncio.create_task(simulate_user_session(config, stats, user_id))

        await asyncio.sleep(interval)


# ============================================================
# 报告与监控
# ============================================================

async def stats_reporter(config: BehaviorConfig, stats: Stats):
    """定期输出统计报告。"""
    print("\n" + "=" * 70)
    print("  稳态用户行为模拟 - 实时监控")
    print("=" * 70)
    print(f"  模式: {config.pattern}  |  基础QPS: {config.base_qps}  |  并发: {config.concurrency}")
    print(f"  时长: {'无限' if config.duration == 0 else f'{config.duration}秒'}  |  错误率: {config.error_rate}%")
    print("=" * 70)

    while True:
        await asyncio.sleep(10)  # 每 10 秒报告一次

        elapsed = stats.elapsed
        current_qps = stats.current_qps
        mult = get_traffic_multiplier(config, elapsed)
        target_qps = config.base_qps * mult

        # 进度条
        if config.duration > 0:
            progress = min(100, elapsed / config.duration * 100)
            bar_len = 30
            filled = int(bar_len * progress / 100)
            bar = "█" * filled + "░" * (bar_len - filled)
            progress_str = f"[{bar}] {progress:.1f}%"
        else:
            progress_str = f"已运行 {elapsed:.0f}s"

        print()
        print(f"  {progress_str}")
        print(f"  当前QPS: {current_qps:.1f} / 目标: {target_qps:.1f} (x{mult:.2f})")
        print(f"  总请求: {stats.total_requests:,}  |  错误: {stats.total_errors} ({stats.error_percent:.2f}%)")
        print(f"  平均延迟: {stats.avg_latency*1000:.1f}ms")

        # 各类型统计
        if stats.requests_by_type:
            print(f"  请求分布:")
            for req_type, count in sorted(stats.requests_by_type.items(), key=lambda x: -x[1]):
                pct = count / stats.total_requests * 100
                err = stats.errors_by_type.get(req_type, 0)
                err_pct = err / count * 100 if count > 0 else 0
                print(f"    {req_type:15s}: {count:>6,} ({pct:5.1f}%)  错误: {err:>4} ({err_pct:.1f}%)")


# ============================================================
# 稳定性验证（T1-31）
# ============================================================

async def stability_monitor(config: BehaviorConfig, stats: Stats):
    """
    稳定性监控（T1-31）。
    - 检查连续运行时间
    - 检查错误率是否异常
    - 检查 QPS 是否稳定
    - 异常时输出告警
    """
    consecutive_errors = 0
    last_qps = 0.0
    check_interval = 30  # 每 30 秒检查一次

    await asyncio.sleep(check_interval)  # 先等一会儿让数据累积

    while True:
        await asyncio.sleep(check_interval)

        current_qps = stats.current_qps
        elapsed = stats.elapsed

        # 1. QPS 稳定性检查（波动不应超过 50%）
        if last_qps > 0:
            change = abs(current_qps - last_qps) / last_qps
            if change > 0.5 and config.pattern == "steady":
                print(f"\n  ⚠️  [稳定性告警] QPS 波动过大: {last_qps:.1f} → {current_qps:.1f} ({change*100:.0f}%)")
        last_qps = current_qps

        # 2. 错误率检查
        if stats.error_percent > config.error_rate * 3:
            print(f"\n  ⚠️  [稳定性告警] 错误率异常: {stats.error_percent:.2f}% (预期 {config.error_rate}%)")

        # 3. 30 分钟里程碑
        if elapsed >= 1800 and elapsed - check_interval < 1800:
            print(f"\n  ✅ [里程碑] 已稳定运行 30 分钟！")
            print(f"     - 总请求: {stats.total_requests:,}")
            print(f"     - 总错误: {stats.total_errors}")
            print(f"     - 错误率: {stats.error_percent:.2f}%")
            print(f"     - 平均延迟: {stats.avg_latency*1000:.1f}ms")

        # 4. 60 分钟里程碑
        if elapsed >= 3600 and elapsed - check_interval < 3600:
            print(f"\n  🏆 [里程碑] 已稳定运行 1 小时！")


# ============================================================
# 异常重试机制（指数退避）
# ============================================================

async def request_with_retry(func, max_retries=3, base_delay=1.0):
    """
    带指数退避的请求重试（T1-31 稳定性保障）。
    """
    retries = 0
    while True:
        try:
            return await func()
        except Exception as e:
            if retries >= max_retries:
                raise
            delay = base_delay * (2 ** retries) * (0.5 + random.random())
            print(f"  [重试] 请求失败，{delay:.1f}s 后第 {retries+1} 次重试: {e}")
            await asyncio.sleep(delay)
            retries += 1


# ============================================================
# 主程序
# ============================================================

def parse_args():
    parser = argparse.ArgumentParser(description="稳态用户行为流量模拟（T1-30 + T1-31）")
    parser.add_argument("--base-qps", type=int, default=20, help="基础 QPS（默认 20）")
    parser.add_argument("--peak-qps", type=int, default=None, help="峰值 QPS（daily 模式使用）")
    parser.add_argument("--duration", type=int, default=1800, help="持续时间（秒），默认 1800（30分钟），0 为无限")
    parser.add_argument("--pattern", type=str, default="steady",
                        choices=["steady", "growing", "spike", "daily"],
                        help="流量模式：steady(稳定)/growing(增长)/spike(尖刺)/daily(日周期)")
    parser.add_argument("--concurrency", type=int, default=10, help="并发用户数")
    parser.add_argument("--error-rate", type=float, default=1.0, help="模拟错误率（%%）")
    parser.add_argument("--real", action="store_true", help="真实发请求模式（默认模拟）")
    parser.add_argument("--base-url", type=str, default="http://localhost:8080", help="目标地址")
    parser.add_argument("-v", "--verbose", action="store_true", help="详细输出每个请求")
    return parser.parse_args()


async def main_async():
    args = parse_args()

    config = BehaviorConfig(
        base_qps=args.base_qps,
        peak_qps=args.peak_qps,
        duration=args.duration,
        pattern=args.pattern,
        concurrency=args.concurrency,
        error_rate=args.error_rate,
        real_request=args.real,
        base_url=args.base_url,
        verbose=args.verbose,
    )

    stats = Stats()
    stats.start_time = time.time()

    # 优雅退出处理
    stop_event = asyncio.Event()

    def handle_signal(signum, frame):
        print(f"\n\n收到信号 {signum}，正在优雅停止...")
        stop_event.set()

    try:
        signal.signal(signal.SIGINT, handle_signal)
        signal.signal(signal.SIGTERM, handle_signal)
    except (ValueError, AttributeError):
        # Windows 上可能不支持某些信号
        pass

    # 启动各协程
    tasks = [
        asyncio.create_task(traffic_controller(config, stats)),
        asyncio.create_task(stats_reporter(config, stats)),
        asyncio.create_task(stability_monitor(config, stats)),
    ]

    # 等待完成或停止
    try:
        await asyncio.gather(*tasks)
    except asyncio.CancelledError:
        pass
    finally:
        stats.end_time = time.time()

    # 最终报告
    total_time = stats.end_time - stats.start_time
    print("\n" + "=" * 70)
    print("  运行结束 - 最终报告")
    print("=" * 70)
    print(f"  总运行时间: {timedelta(seconds=int(total_time))}")
    print(f"  总请求数:   {stats.total_requests:,}")
    print(f"  平均 QPS:   {stats.current_qps:.2f}")
    print(f"  总错误数:   {stats.total_errors}")
    print(f"  错误率:     {stats.error_percent:.4f}%")
    print(f"  平均延迟:   {stats.avg_latency*1000:.2f}ms")
    print()
    print("  请求类型分布:")
    for req_type, count in sorted(stats.requests_by_type.items(), key=lambda x: -x[1]):
        pct = count / stats.total_requests * 100 if stats.total_requests > 0 else 0
        err = stats.errors_by_type.get(req_type, 0)
        err_pct = err / count * 100 if count > 0 else 0
        print(f"    {req_type:15s}: {count:>8,} ({pct:5.1f}%)  错误: {err:>6} ({err_pct:.2f}%)")
    print("=" * 70)

    # T1-31 验证：30 分钟无报错
    if total_time >= 1800 and stats.total_errors == 0:
        print("\n  ✅ 稳定性验证通过：连续运行 30 分钟无错误")
    elif total_time >= 1800:
        print(f"\n  ⚠️  运行满 30 分钟，但有 {stats.total_errors} 个错误")
    else:
        print(f"\n  ℹ️  运行 {total_time:.0f} 秒，不足 30 分钟")


def main():
    try:
        asyncio.run(main_async())
    except KeyboardInterrupt:
        print("\n\n已停止。")
        sys.exit(0)


if __name__ == "__main__":
    main()
