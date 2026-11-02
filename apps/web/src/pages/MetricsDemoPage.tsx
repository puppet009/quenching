import { useState, useEffect, useCallback } from "react";
import { RealtimeChart } from "@quenching/shared";
import { ServiceHealthGrid } from "../components/ServiceHealthCard";
import { AlertList } from "../components/AlertList";
import { LogViewer } from "../components/LogViewer";
import { ConfigViewer } from "../components/ConfigViewer";
import {
  ALL_METRICS,
  METRIC_CONFIG,
  generateMockData,
  generateMockHealthData,
  type DataPoint,
  type MetricName,
  type ServiceHealth,
} from "@quenching/shared";
import {
  generateMockAlerts,
  generateMockNewAlert,
  type AlertItem,
} from "../services/alerts";
import {
  generateMockLogs,
  generateMockNewLogs,
  type LogItem,
} from "../services/logs";
import {
  generateMockConfigFiles,
  generateMockDeployInfo,
  type ConfigFile,
  type ServiceDeployInfo,
} from "../services/configs";

const SERVICES = ["全部服务", "order-service", "product-service", "user-service", "payment-service"];
const HEALTH_SERVICES = ["order-service", "product-service", "user-service", "payment-service", "ai-assistant-service"];
const LOG_SERVICES = ["order-service", "product-service", "user-service", "payment-service"];
const CONFIG_SERVICES = ["order-service", "product-service", "user-service", "payment-service"];

/**
 * 实时图表组件演示页（T2-19）
 *
 * 展示 RealtimeChart 组件：
 * - 5 大核心指标（CPU/内存/QPS/错误率/P99）
 * - 服务切换
 * - 实时刷新模拟（每 3 秒追加新数据点）
 * - 鼠标悬停 tooltip
 * - 降采样演示
 */
export function MetricsDemoPage() {
  const [selectedService, setSelectedService] = useState<string>("全部服务");
  const [isLive, setIsLive] = useState(true);
  const [isLoading] = useState(false);
  const [healthData, setHealthData] = useState<ServiceHealth[]>(() =>
    generateMockHealthData(HEALTH_SERVICES),
  );
  const [alertData, setAlertData] = useState<AlertItem[]>(() => generateMockAlerts(8));
  const [logData, setLogData] = useState<LogItem[]>(() => generateMockLogs(80));
  const [logService, setLogService] = useState<string>("");
  const [configFiles, setConfigFiles] = useState<ConfigFile[]>(() =>
    generateMockConfigFiles(CONFIG_SERVICES[0]),
  );
  const [configDeploy, setConfigDeploy] = useState<ServiceDeployInfo | undefined>(() =>
    generateMockDeployInfo(CONFIG_SERVICES[0]),
  );
  const [configService, setConfigService] = useState<string>(CONFIG_SERVICES[0]);
  const [metricData, setMetricData] = useState<Record<MetricName, DataPoint[]>>(() => {
    const initial: Record<string, DataPoint[]> = {};
    ALL_METRICS.forEach((m) => {
      initial[m] = generateMockData(m, 15 * 60 * 1000, 15000);
    });
    return initial as Record<MetricName, DataPoint[]>;
  });

  // 模拟实时数据推送
  const pushNewPoint = useCallback(() => {
    setMetricData((prev) => {
      const next: Record<string, DataPoint[]> = {};
      const now = Date.now();

      ALL_METRICS.forEach((metric) => {
        const prevPoints = prev[metric];
        const lastValue = prevPoints.length > 0 ? prevPoints[prevPoints.length - 1].value : 50;
        const config = METRIC_CONFIG[metric];

        // 基于上一个值的随机游走
        const drift = (Math.random() - 0.5) * (config.max ?? 100) * 0.05;
        let newValue = lastValue + drift;

        // 边界约束
        if (config.min !== undefined && newValue < config.min) {
          newValue = config.min + Math.abs(drift);
        }
        if (config.max !== undefined && newValue > config.max) {
          newValue = config.max - Math.abs(drift);
        }
        if (newValue < 0) newValue = 0;

        const newPoint: DataPoint = { ts: now, value: newValue };

        // 最多保留 15 分钟数据（15*60/15 = 60 个点）
        const newPoints = [...prevPoints, newPoint];
        if (newPoints.length > 120) {
          newPoints.splice(0, newPoints.length - 120);
        }

        next[metric] = newPoints;
      });

      return next as Record<MetricName, DataPoint[]>;
    });
  }, []);

  useEffect(() => {
    if (!isLive) return;
    const timer = setInterval(pushNewPoint, 3000);
    return () => clearInterval(timer);
  }, [isLive, pushNewPoint]);

  // 模拟健康数据定时刷新
  useEffect(() => {
    if (!isLive) return;
    const timer = setInterval(() => {
      setHealthData(generateMockHealthData(HEALTH_SERVICES));
    }, 5000);
    return () => clearInterval(timer);
  }, [isLive]);

  // 模拟告警实时推送：每 8 秒新增一条告警，部分旧告警自动恢复
  useEffect(() => {
    if (!isLive) return;
    const timer = setInterval(() => {
      setAlertData((prev) => {
        let next = [...prev];

        // 30% 概率将一条触发中的告警标记为已恢复
        const firingAlerts = next.filter((a) => a.status === "firing");
        if (firingAlerts.length > 0 && Math.random() > 0.7) {
          const randomFiring = firingAlerts[Math.floor(Math.random() * firingAlerts.length)];
          next = next.map((a) =>
            a.id === randomFiring.id
              ? { ...a, status: "resolved" as const, resolved_at: Date.now() }
              : a,
          );
        }

        // 新增一条告警
        const newAlert = generateMockNewAlert();
        next.unshift(newAlert);

        // 最多保留 30 条
        if (next.length > 30) next = next.slice(0, 30);

        return next;
      });
    }, 8000);
    return () => clearInterval(timer);
  }, [isLive]);

  // 模拟日志实时刷新：每 2 秒追加新日志
  useEffect(() => {
    if (!isLive) return;
    const timer = setInterval(() => {
      setLogData((prev) => {
        const newLogs = generateMockNewLogs(3, logService || undefined);
        const next = [...newLogs, ...prev];
        // 最多保留 200 条
        if (next.length > 200) next.length = 200;
        return next;
      });
    }, 2000);
    return () => clearInterval(timer);
  }, [isLive, logService]);

  // 日志服务切换时重新生成数据
  useEffect(() => {
    setLogData(generateMockLogs(80, logService ? { service: logService } : undefined));
  }, [logService]);

  // 配置服务切换时重新生成数据
  useEffect(() => {
    if (!configService) return;
    setConfigFiles(generateMockConfigFiles(configService));
    setConfigDeploy(generateMockDeployInfo(configService));
  }, [configService]);

  return (
    <div className="min-h-screen bg-ink-50 py-8 px-4">
      <div className="page-container">
        {/* 标题栏 */}
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 mb-6">
          <div>
            <h1 className="text-2xl font-bold text-ink-900 mb-1">实时监控</h1>
            <p className="text-[13px] text-ink-500">
              5 大核心指标实时曲线 · 支持多服务切换 · 数据点降采样
            </p>
          </div>

          <div className="flex items-center gap-3">
            {/* 服务选择 */}
            <select
              value={selectedService}
              onChange={(e) => setSelectedService(e.target.value)}
              className="px-3 py-2 text-sm bg-white border border-ink-200 rounded-lg focus:outline-none focus:ring-2 focus:ring-brand-500/30 focus:border-brand-400"
            >
              {SERVICES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>

            {/* 实时开关 */}
            <button
              onClick={() => setIsLive(!isLive)}
              className={`px-3 py-2 text-sm font-medium rounded-lg flex items-center gap-2 transition-colors ${
                isLive
                  ? "bg-ok-50 text-ok-700 border border-ok-200 hover:bg-ok-100"
                  : "bg-ink-100 text-ink-600 border border-ink-200 hover:bg-ink-200"
              }`}
            >
              <span
                className={`w-2 h-2 rounded-full ${
                  isLive ? "bg-ok-500 animate-pulse" : "bg-ink-400"
                }`}
              />
              {isLive ? "实时中" : "已暂停"}
            </button>
          </div>
        </div>

        {/* 健康 + 告警：两列布局 */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-6">
          {/* 服务健康状态 - 占 2 列 */}
          <div className="lg:col-span-2">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h3 className="text-base font-bold text-ink-900">服务健康状态</h3>
                <p className="text-[12px] text-ink-500">核心服务实时健康巡检 · 每 5 秒刷新</p>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-[11px] text-ok-600 font-medium">
                  {healthData.filter((h) => h.healthy).length}/{healthData.length} 正常
                </span>
              </div>
            </div>
            <ServiceHealthGrid
              services={healthData}
              loading={isLoading}
              onServiceClick={(s) => setSelectedService(s)}
            />
          </div>

          {/* 实时告警 - 占 1 列 */}
          <div>
            <AlertList
              alerts={alertData}
              loading={isLoading}
              maxItems={8}
              showFilter={false}
              onAlertClick={(a) => setSelectedService(a.service)}
            />
          </div>
        </div>

        {/* 图表网格：2 列 */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-6">
          {ALL_METRICS.map((metric) => (
            <MetricCard
              key={metric}
              metric={metric}
              data={metricData[metric]}
              service={selectedService}
            />
          ))}
        </div>

        {/* 大图展示区：QPS + 错误率 */}
        <div className="grid grid-cols-1 gap-4 mb-6">
          <div className="bg-white rounded-2xl border border-ink-100 shadow-sm p-5">
            <div className="flex items-center justify-between mb-3">
              <div>
                <h3 className="text-base font-bold text-ink-900">QPS 趋势</h3>
                <p className="text-[12px] text-ink-500">最近 15 分钟 · 每 3 秒刷新</p>
              </div>
              <div className="flex items-center gap-2">
                <TimeRangeBadge label="15m" active />
                <TimeRangeBadge label="1h" />
                <TimeRangeBadge label="6h" />
              </div>
            </div>
            <RealtimeChart
              data={metricData.qps}
              metric="qps"
              height={280}
              showCurrentValue
              maxPoints={200}
            />
          </div>
        </div>

        {/* 日志查看面板 */}
        <div className="mb-6">
          <div className="flex items-center justify-between mb-3">
            <div>
              <h3 className="text-base font-bold text-ink-900">实时日志</h3>
              <p className="text-[12px] text-ink-500">按服务/级别/关键字检索 · 实时滚动 · 每 2 秒刷新</p>
            </div>
          </div>
          <LogViewer
            logs={logData}
            loading={isLoading}
            services={LOG_SERVICES}
            selectedService={logService}
            onServiceChange={setLogService}
            onLiveToggle={setIsLive}
            isLive={isLive}
            maxLines={150}
          />
        </div>

        {/* 配置查看面板 */}
        <div className="mb-6">
          <div className="flex items-center justify-between mb-3">
            <div>
              <h3 className="text-base font-bold text-ink-900">服务配置</h3>
              <p className="text-[12px] text-ink-500">只读查看配置文件与部署信息 · 敏感字段脱敏</p>
            </div>
          </div>
          <ConfigViewer
            files={configFiles}
            deployInfo={configDeploy}
            service={configService}
            services={CONFIG_SERVICES}
            onServiceChange={setConfigService}
            loading={isLoading}
          />
        </div>

        {/* 功能说明 */}
        <div className="bg-white rounded-2xl border border-ink-100 p-5">
          <h3 className="text-sm font-bold text-ink-800 mb-3">组件特性</h3>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-[13px]">
            <FeatureItem icon="📈" title="贝塞尔曲线" desc="平滑曲线渲染" />
            <FeatureItem icon="🔻" title="自动降采样" desc="大数据量不卡顿" />
            <FeatureItem icon="🖱️" title="悬停详情" desc="Tooltip 显示精确值" />
            <FeatureItem icon="⚡" title="零依赖" desc="纯 SVG 实现" />
          </div>
        </div>
      </div>
    </div>
  );
}

/* ===== 子组件：指标卡片 ===== */

function MetricCard({
  metric,
  data,
  service,
}: {
  metric: MetricName;
  data: DataPoint[];
  service: string;
}) {
  const config = METRIC_CONFIG[metric];
  const current = data.length > 0 ? data[data.length - 1].value : 0;
  const first = data.length > 0 ? data[0].value : 0;
  const diff = current - first;
  const diffPct = first !== 0 ? (diff / first) * 100 : 0;

  return (
    <div className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden hover:shadow-md transition-shadow">
      <div className="px-5 pt-4 pb-2 flex items-start justify-between">
        <div>
          <div className="text-[12px] text-ink-400 mb-1">{config.displayName}</div>
          <div
            className="text-2xl font-bold leading-tight"
            style={{ color: `rgb(${config.color})` }}
          >
            {config.formatValue(current)}
          </div>
          <div className={`text-[11px] mt-1 font-medium ${
            diff >= 0 ? "text-danger-500" : "text-ok-500"
          }`}>
            {diff >= 0 ? "↑" : "↓"} {Math.abs(diffPct).toFixed(1)}% 较开始
          </div>
        </div>
        <div
          className="w-10 h-10 rounded-xl flex items-center justify-center text-lg"
          style={{ backgroundColor: `rgba(${config.color}, 0.1)` }}
        >
          {metric === "cpu_usage" && "💻"}
          {metric === "memory_usage" && "🧠"}
          {metric === "qps" && "⚡"}
          {metric === "error_rate" && "⚠️"}
          {metric === "p99_latency" && "⏱️"}
        </div>
      </div>
      <div className="px-2">
        <RealtimeChart
          data={data}
          metric={metric}
          height={120}
          showYAxis={false}
          showXAxis={false}
          showCurrentValue={false}
          maxPoints={60}
        />
      </div>
      <div className="px-5 pb-3 pt-1 flex items-center justify-between border-t border-ink-50">
        <span className="text-[11px] text-ink-400">{service}</span>
        <span className="text-[11px] text-ink-400">
          {data.length} 个数据点
        </span>
      </div>
    </div>
  );
}

/* ===== 子组件：时间范围徽章 ===== */

function TimeRangeBadge({ label, active = false }: { label: string; active?: boolean }) {
  return (
    <button
      className={`px-3 py-1 text-[12px] rounded-lg font-medium transition-colors ${
        active
          ? "bg-brand-500 text-white"
          : "bg-ink-100 text-ink-500 hover:bg-ink-200"
      }`}
    >
      {label}
    </button>
  );
}

/* ===== 子组件：特性项 ===== */

function FeatureItem({ icon, title, desc }: { icon: string; title: string; desc: string }) {
  return (
    <div className="flex items-start gap-2">
      <span className="text-lg">{icon}</span>
      <div>
        <div className="text-[13px] font-semibold text-ink-700">{title}</div>
        <div className="text-[11px] text-ink-500">{desc}</div>
      </div>
    </div>
  );
}
