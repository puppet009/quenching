#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Go 服务标准目录结构护栏（对应 README 第四节）。

规则：每个 Go 服务的 internal/ 下只允许七个标准一级目录：
    handler / service / repository / model / dto / config / pkg

迁移期处理：对尚未完成归位的服务，在 EXEMPTIONS 里登记“当前已知野包”作为
基线。本脚本只拦截基线之外【新长出来】的野包；每搬完一个服务，就把该服务的
豁免清空，最终全部服务回到零豁免即严格模式。

退出码：0 通过；1 发现违规目录。
"""

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SERVICES_DIR = ROOT / "services"

# 标准一级目录（README §4.1）
STANDARD_DIRS = {"handler", "service", "repository", "model", "dto", "config", "pkg"}

# 不参与本检查的服务：
# - ai-service：Python 服务，结构标准不同（README §4.2）
# （ws-gateway 已于 g7 并回 api-gateway，不再有独立服务）
EXCLUDED_SERVICES = {"ai-service"}

# 迁移基线：服务 -> 当前允许存在的非标准一级目录集合。
# 归位一个服务后删除其条目。禁止为容纳新代码而向此处追加目录。
EXEMPTIONS = {
    "api-gateway": {"middleware", "proxy"},
}


def discover_services():
    """发现含 go.mod 的后端服务目录。"""
    services = []
    for child in sorted(SERVICES_DIR.iterdir()):
        if child.is_dir() and (child / "go.mod").exists():
            if child.name not in EXCLUDED_SERVICES:
                services.append(child.name)
    return services


def check_service(service):
    """返回 (违规目录列表, 剩余豁免野包列表)。"""
    internal = SERVICES_DIR / service / "internal"
    if not internal.is_dir():
        return [], []

    actual = {p.name for p in internal.iterdir() if p.is_dir()}
    wild = actual - STANDARD_DIRS
    exempted = set(EXEMPTIONS.get(service, set()))

    violations = sorted(wild - exempted)
    remaining = sorted(wild & exempted)
    return violations, remaining


def main():
    services = discover_services()
    total_violations = []
    print("== Go 服务 internal/ 标准目录检查 ==")
    print(f"标准目录: {sorted(STANDARD_DIRS)}\n")

    for service in services:
        violations, remaining = check_service(service)
        if violations:
            total_violations.extend((service, v) for v in violations)
            print(f"[FAIL] {service}: 未登记的非标准目录 {violations}")
            print("       请迁入 handler/service/repository/model/dto/config/pkg，")
            print("       不要新增野包（如确有必要，先更新 README 与本脚本基线）。")
        elif remaining:
            print(f"[WARN] {service}: 待归位野包 {remaining}（迁移基线，禁止新增）")
        else:
            print(f"[ OK ] {service}: 目录结构合规")

    print()
    if total_violations:
        print(f"结果：发现 {len(total_violations)} 个违规目录，结构检查失败。")
        return 1

    print("结果：无新增野包（待归位项以 WARN 列出，归位后收紧基线）。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
