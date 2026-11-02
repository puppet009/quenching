import {formatAbsTime, nowMs} from "../utils/time";
// 核心指标 service（T2-19）
// 对应 training-service 的 metrics handler + api-gateway `/ws` 的 metrics 事件（g7 起 ws-gateway 已并入 api-gateway）

/** 5 大核心指标名称 */
export type MetricName =
  | "cpu_usage"    // CPU 使用率
  | "memory_usage" // 内存使用率
  | "qps"          // 每秒请求数
  | "error_rate"   // 错误率
  | "p99_latency"; // P99 请求延迟

/** 指标定义 */
export interface MetricDef {
  name: MetricName;
  displayName: string;
  unit: string;
  category: "system" | "business";
  description: string;
}

/** 单个数据点 */
export interface DataPoint {
  /** 时间戳（毫秒） */
  ts: number;
  /** 指标值 */
  value: number;
}

/** 区间查询结果 */
export interface QueryResult {
  metric: MetricName;
  unit: string;
  service: string;
  instance: string;
  points: DataPoint[];
}

/** 瞬时值查询结果 */
export interface InstantResult {
  metric: MetricName;
  unit: string;
  service: string;
  value: number;
  at: number;
}

/** 指标快照（所有指标当前值） */
export interface MetricsSnapshot {
  cpu_usage: number;
  memory_usage: number;
  qps: number;
  error_rate: number;
  p99_latency: number;
}

/** 聚合方式 */
export type Aggregation = "avg" | "max" | "min" | "last";

/** 区间查询参数 */
export interface QueryParams {
  metric: MetricName;
  service?: string;
  instance?: string;
  startTime?: number; // 毫秒时间戳
  endTime?: number;   // 毫秒时间戳
  step?: number;      // 采样步长（秒）
  aggregation?: Aggregation;
}

/** 指标显示配置 */
export const METRIC_CONFIG: Record<MetricName, {
  displayName: string;
  unit: string;
  category: "system" | "business";
  color: string;  // 图表颜色（RGB，不带 #）
  min?: number;
  max?: number;
  formatValue: (v: number) => string;
}> = {
  cpu_usage: {
    displayName: "CPU 使用率",
    unit: "%",
    category: "system",
    color: "139, 92, 246",
    min: 0,
    max: 100,
    formatValue: (v) => v.toFixed(1) + "%",
  },
  memory_usage: {
    displayName: "内存使用率",
    unit: "%",
    category: "system",
    color: "16, 185, 129",
    min: 0,
    max: 100,
    formatValue: (v) => v.toFixed(1) + "%",
  },
  qps: {
    displayName: "QPS",
    unit: "次/秒",
    category: "business",
    color: "59, 130, 246",
    formatValue: (v) => (v >= 1000 ? (v / 1000).toFixed(1) + "k" : v.toFixed(0)),
  },
  error_rate: {
    displayName: "错误率",
    unit: "%",
    category: "business",
    color: "239, 68, 68",
    min: 0,
    formatValue: (v) => v.toFixed(2) + "%",
  },
  p99_latency: {
    displayName: "P99 延迟",
    unit: "秒",
    category: "business",
    color: "245, 158, 11",
    formatValue: (v) => (v >= 1 ? v.toFixed(2) + "s" : (v * 1000).toFixed(0) + "ms"),
  },
};

/** 所有指标名称列表 */
export const ALL_METRICS: MetricName[] = [
  "cpu_usage",
  "memory_usage",
  "qps",
  "error_rate",
  "p99_latency",
];

/** 服务健康状态 */
export interface ServiceHealth {
  service: string;
  healthy: boolean;
  cpu_usage: number;
  memory_usage: number;
  error_rate: number;
  reason?: string;
}

/** 服务健康状态等级 */
export type HealthLevel = "healthy" | "warning" | "critical";

/** 根据健康数据计算等级 */
export function getHealthLevel(h: ServiceHealth): HealthLevel {
  if (!h.healthy) return "critical";
  if (h.cpu_usage > 80 || h.memory_usage > 80 || h.error_rate > 2) return "warning";
  return "healthy";
}

/** 健康状态显示配置 */
export const HEALTH_CONFIG: Record<HealthLevel, {
  label: string;
  dotColor: string;
  textColor: string;
  bgColor: string;
  borderColor: string;
  pulseColor?: string;
}> = {
  healthy: {
    label: "运行正常",
    dotColor: "bg-ok-500",
    textColor: "text-ok-700",
    bgColor: "bg-ok-50",
    borderColor: "border-ok-200",
  },
  warning: {
    label: "注意",
    dotColor: "bg-warn-500",
    textColor: "text-warn-700",
    bgColor: "bg-warn-50",
    borderColor: "border-warn-200",
    pulseColor: "bg-warn-400",
  },
  critical: {
    label: "异常",
    dotColor: "bg-danger-500",
    textColor: "text-danger-700",
    bgColor: "bg-danger-50",
    borderColor: "border-danger-300",
    pulseColor: "bg-danger-400",
  },
};

/* ===== API 封装 ===== */

const API_BASE = "/api/v1/metrics";

/** 获取指标定义列表 */
export async function listMetricDefinitions(): Promise<MetricDef[]> {
  const res = await fetch(`${API_BASE}/definitions`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取指标定义失败");
  return json.data;
}

/** 区间查询指标数据 */
export async function queryMetric(params: QueryParams): Promise<QueryResult> {
  const search = new URLSearchParams();
  search.set("metric", params.metric);
  if (params.service) search.set("service", params.service);
  if (params.instance) search.set("instance", params.instance);
  if (params.startTime) search.set("start_time", String(params.startTime));
  if (params.endTime) search.set("end_time", String(params.endTime));
  if (params.step) search.set("step", String(params.step));
  if (params.aggregation) search.set("aggregation", params.aggregation);

  const res = await fetch(`${API_BASE}/query?${search.toString()}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "指标查询失败");
  return json.data;
}

/** 获取指标当前值 */
export async function getCurrentMetric(
  metric: MetricName,
  service?: string,
  aggregation: Aggregation = "avg",
): Promise<InstantResult> {
  const search = new URLSearchParams();
  search.set("metric", metric);
  if (service) search.set("service", service);
  search.set("aggregation", aggregation);

  const res = await fetch(`${API_BASE}/current?${search.toString()}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取当前值失败");
  return json.data;
}

/** 获取所有指标快照 */
export async function getMetricsSnapshot(service?: string): Promise<MetricsSnapshot> {
  const search = new URLSearchParams();
  if (service) search.set("service", service);

  const res = await fetch(`${API_BASE}/snapshot?${search.toString()}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取快照失败");
  return json.data;
}

/** 获取服务健康状态 */
export async function getServiceHealth(service: string): Promise<ServiceHealth> {
  const res = await fetch(`${API_BASE}/health?service=${encodeURIComponent(service)}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "健康检查失败");
  return json.data;
}

/* ===== 数据处理工具 ===== */

/**
 * 数据点降采样（LTTB 简化版：按时间窗口取平均）
 * 当数据点超过 maxPoints 时，按窗口平均降采样
 */
export function downsample(points: DataPoint[], maxPoints: number): DataPoint[] {
  if (points.length <= maxPoints) return points;

  const bucketSize = Math.ceil(points.length / maxPoints);
  const result: DataPoint[] = [];

  for (let i = 0; i < points.length; i += bucketSize) {
    const bucket = points.slice(i, i + bucketSize);
    const avgValue = bucket.reduce((sum, p) => sum + p.value, 0) / bucket.length;
    const midTs = bucket[Math.floor(bucket.length / 2)].ts;
    result.push({ ts: midTs, value: avgValue });
  }

  return result;
}

/** 生成模拟数据（用于 demo/开发，无后端时使用） */
export function generateMockData(
  metric: MetricName,
  durationMs = 15 * 60 * 1000,
  stepMs = 15000,
): DataPoint[] {
  const points: DataPoint[] = [];
  const now = nowMs();
  const count = Math.floor(durationMs / stepMs);

  // 根据指标类型设置基准值和波动范围
  const baseValues: Record<MetricName, { base: number; range: number; trend?: number }> = {
    cpu_usage: { base: 45, range: 20 },
    memory_usage: { base: 60, range: 10, trend: 0.02 },
    qps: { base: 120, range: 50 },
    error_rate: { base: 1.2, range: 0.8 },
    p99_latency: { base: 0.15, range: 0.08 },
  };

  const cfg = baseValues[metric];

  for (let i = 0; i < count; i++) {
    const ts = now - (count - i) * stepMs;
    const noise = (Math.random() - 0.5) * 2 * cfg.range;
    const trend = cfg.trend ? cfg.trend * i : 0;
    let value = cfg.base + noise + trend;

    // 确保非负
    if (value < 0) value = 0;

    points.push({ ts, value });
  }

  return points;
}

/** 生成模拟健康数据（用于 demo/开发，无后端时使用） */
export function generateMockHealthData(services: string[]): ServiceHealth[] {
  return services.map((service, idx) => {
    const cpu = 30 + Math.random() * 60;
    const memory = 40 + Math.random() * 50;
    const errorRate = Math.random() * 3;
    const healthy = idx % 5 !== 3; // 模拟一个异常服务

    return {
      service,
      healthy,
      cpu_usage: Math.round(cpu * 10) / 10,
      memory_usage: Math.round(memory * 10) / 10,
      error_rate: Math.round(errorRate * 100) / 100,
      reason: !healthy ? "服务心跳超时，最近一次检查在 30s 前" : undefined,
    };
  });
}

/** 格式化时间戳为 HH:MM:SS */
export function formatTime(ts: number): string { return formatAbsTime(ts); }
