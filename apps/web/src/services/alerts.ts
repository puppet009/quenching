// 告警服务（T2-21）
// 真实接口对应 training-service（经 api-gateway /api/v1/alerts）：
//   GET /list?sandbox_id=&status=   告警实例列表
//   GET /summary?sandbox_id=        告警统计
// 后端 AlertInstance 与前端 AlertItem 字段/状态不一致，本文件 API 层负责适配。
// 下半部保留 MetricsDemoPage 演示用 mock 与展示工具，真实业务不要引用 mock。

import { http, formatAbsTime, nowMs } from "@quenching/shared";

/** 告警级别 */
export type AlertLevel = "info" | "warning" | "critical";

/** 告警状态 */
export type AlertStatus = "firing" | "resolved";

/** 告警来源：后端指标/日志，或浏览器侧观测 */
export type AlertSource = "backend" | "frontend";

/** 单条告警 */
export interface AlertItem {
  /** 告警唯一 ID */
  id: string;
  /** 告警级别 */
  level: AlertLevel;
  /** 告警标题 */
  title: string;
  /** 告警详情 */
  message: string;
  /** 告警来源：默认 backend */
  source: AlertSource;
  /** 前端告警：失败的行为步骤名（可选） */
  step?: string;
  /** 关联服务 */
  service: string;
  /** 关联指标（可选） */
  metric?: string;
  /** 当前值（可选） */
  current_value?: number;
  /** 阈值（可选） */
  threshold?: number;
  /** 状态：触发中 / 已恢复 */
  status: AlertStatus;
  /** 触发时间戳（毫秒） */
  firing_at: number;
  /** 恢复时间戳（毫秒），仅 resolved 状态有 */
  resolved_at?: number;
}

/** 告警级别显示配置 */
export const ALERT_LEVEL_CONFIG: Record<AlertLevel, {
  label: string;
  dotColor: string;
  textColor: string;
  bgColor: string;
  borderColor: string;
  icon: string;
  pulseColor?: string;
}> = {
  info: {
    label: "提示",
    dotColor: "bg-brand-500",
    textColor: "text-brand-700",
    bgColor: "bg-brand-50",
    borderColor: "border-brand-200",
    icon: "ℹ️",
  },
  warning: {
    label: "警告",
    dotColor: "bg-warn-500",
    textColor: "text-warn-700",
    bgColor: "bg-warn-50",
    borderColor: "border-warn-300",
    icon: "⚠️",
    pulseColor: "bg-warn-400",
  },
  critical: {
    label: "严重",
    dotColor: "bg-danger-500",
    textColor: "text-danger-700",
    bgColor: "bg-danger-50",
    borderColor: "border-danger-300",
    icon: "🚨",
    pulseColor: "bg-danger-400",
  },
};

/* ===== 真实接口（training-service AlertInstance） ===== */

/** 后端告警实例原始结构（service.AlertInstance） */
interface RawAlertInstance {
  id: string;
  rule_id: string;
  rule_name: string;
  severity: AlertLevel;
  /** ok/pending/firing/acknowledged/resolved */
  status: string;
  sandbox_id: string;
  service: string;
  metric: string;
  value: number;
  threshold: number;
  message: string;
  /** RFC3339 字符串（零值为 "0001-01-01T00:00:00Z"） */
  started_at: string;
  fired_at: string;
  resolved_at: string;
  trigger_count: number;
  /** 附加标签：source/step 存放于此 */
  labels?: Record<string, string>;
}

/** 查询告警列表参数（按后端契约） */
export interface AlertQueryParams {
  /** 沙箱标识（训练页必传） */
  sandboxId?: string;
  /** 后端原始状态：ok/pending/firing/acknowledged/resolved */
  rawStatus?: string;
}

/** RFC3339 时间解析为毫秒；零值/非法返回 0 */
function parseTimeMs(v?: string): number {
  if (!v) return 0;
  const t = Date.parse(v);
  return Number.isNaN(t) ? 0 : t;
}

/** 后端 AlertInstance → 前端 AlertItem（状态归一为 firing/resolved） */
function adaptAlertInstance(a: RawAlertInstance): AlertItem {
  const resolved = a.status === "resolved";
  const firingAt = parseTimeMs(a.fired_at) || parseTimeMs(a.started_at) || nowMs();
  const resolvedAt = resolved ? parseTimeMs(a.resolved_at) || undefined : undefined;
  return {
    id: a.id,
    level: a.severity ?? "info",
    title: a.rule_name || "告警",
    message: a.message || a.rule_name,
    source: a.labels?.source === "frontend" ? "frontend" : "backend",
    step: a.labels?.step || undefined,
    service: a.service,
    metric: a.metric || undefined,
    current_value: typeof a.value === "number" ? a.value : undefined,
    threshold: typeof a.threshold === "number" && a.threshold > 0 ? a.threshold : undefined,
    status: resolved ? "resolved" : "firing",
    firing_at: firingAt,
    resolved_at: resolvedAt,
  };
}

/**
 * 列出告警实例并适配为前端结构。
 * 后端返回数组（非 {items,total}）；过滤掉 ok 态与无时间戳的噪声，并按时间倒序。
 */
export async function listAlerts(params: AlertQueryParams = {}): Promise<AlertItem[]> {
  const query: Record<string, string> = {};
  if (params.sandboxId) query.sandbox_id = params.sandboxId;
  if (params.rawStatus) query.status = params.rawStatus;
  const rows = await http.get<unknown, RawAlertInstance[]>("/alerts/list", { params: query });
  return (rows ?? [])
    .map(adaptAlertInstance)
    .filter((a) => a.status === "firing" || Boolean(a.resolved_at))
    .sort((x, y) => (y.resolved_at ?? y.firing_at) - (x.resolved_at ?? x.firing_at));
}

/** 告警统计（后端 /alerts/summary 扁平 map） */
export interface AlertSummary {
  total: number;
  firing: number;
  pending: number;
  resolved: number;
  acknowledged: number;
  critical: number;
  warning: number;
  info: number;
}

/** 获取告警统计（可按沙箱过滤） */
export function getAlertSummary(sandboxId?: string): Promise<AlertSummary> {
  return http.get<unknown, AlertSummary>("/alerts/summary", {
    params: sandboxId ? { sandbox_id: sandboxId } : undefined,
  });
}

/* ===== Mock 数据生成 ===== */

const MOCK_SERVICES = ["order-service", "product-service", "user-service", "payment-service"];

const MOCK_ALERT_TEMPLATES: Array<{
  level: AlertLevel;
  title: string;
  message: string;
  metric?: string;
  threshold?: number;
}> = [
  {
    level: "critical",
    title: "CPU 使用率过高",
    message: "服务 CPU 使用率持续超过 90%，可能影响响应速度",
    metric: "cpu_usage",
    threshold: 90,
  },
  {
    level: "critical",
    title: "错误率飙升",
    message: "5xx 错误率超过 5%，用户请求大量失败",
    metric: "error_rate",
    threshold: 5,
  },
  {
    level: "warning",
    title: "内存使用率偏高",
    message: "内存使用率超过 80%，需关注是否存在泄漏",
    metric: "memory_usage",
    threshold: 80,
  },
  {
    level: "warning",
    title: "P99 延迟升高",
    message: "P99 请求延迟超过 500ms，用户体验下降",
    metric: "p99_latency",
    threshold: 0.5,
  },
  {
    level: "warning",
    title: "QPS 突增",
    message: "请求量较基线增长 50% 以上，可能存在流量异常",
    metric: "qps",
    threshold: 200,
  },
  {
    level: "info",
    title: "服务重启",
    message: "检测到服务实例重启，请确认是否为预期操作",
  },
  {
    level: "critical",
    title: "服务不可达",
    message: "健康检查连续失败，服务可能已宕机",
  },
];

let mockAlertIdCounter = 0;

/** 生成模拟告警数据 */
export function generateMockAlerts(count = 8): AlertItem[] {
  const alerts: AlertItem[] = [];
  const now = nowMs();

  for (let i = 0; i < count; i++) {
    const template = MOCK_ALERT_TEMPLATES[Math.floor(Math.random() * MOCK_ALERT_TEMPLATES.length)];
    const service = MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)];
    const isResolved = Math.random() > 0.6; // 约 40% 已恢复
    const firingAt = now - Math.random() * 30 * 60 * 1000; // 30分钟内
    const resolvedAt = isResolved ? firingAt + Math.random() * 10 * 60 * 1000 : undefined;

    let currentValue: number | undefined;
    if (template.metric && template.threshold) {
      if (template.metric === "cpu_usage" || template.metric === "memory_usage") {
        currentValue = Math.round((template.threshold + Math.random() * 15) * 10) / 10;
      } else if (template.metric === "error_rate") {
        currentValue = Math.round((template.threshold + Math.random() * 3) * 100) / 100;
      } else if (template.metric === "p99_latency") {
        currentValue = Math.round((template.threshold + Math.random() * 0.5) * 1000) / 1000;
      } else if (template.metric === "qps") {
        currentValue = Math.round(template.threshold * (1 + Math.random() * 0.5));
      }
    }

    alerts.push({
      id: `alert-${++mockAlertIdCounter}`,
      level: template.level,
      title: template.title,
      message: template.message,
      source: "backend",
      service,
      metric: template.metric,
      current_value: currentValue,
      threshold: template.threshold,
      status: isResolved ? "resolved" : "firing",
      firing_at: firingAt,
      resolved_at: resolvedAt,
    });
  }

  // 按时间倒序
  return alerts.sort((a, b) => (b.resolved_at ?? b.firing_at) - (a.resolved_at ?? a.firing_at));
}

/** 生成一条新的模拟告警（用于模拟实时推送） */
export function generateMockNewAlert(): AlertItem {
  const template = MOCK_ALERT_TEMPLATES[Math.floor(Math.random() * MOCK_ALERT_TEMPLATES.length)];
  const service = MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)];

  let currentValue: number | undefined;
  if (template.metric && template.threshold) {
    if (template.metric === "cpu_usage" || template.metric === "memory_usage") {
      currentValue = Math.round((template.threshold + Math.random() * 10) * 10) / 10;
    } else if (template.metric === "error_rate") {
      currentValue = Math.round((template.threshold + Math.random() * 2) * 100) / 100;
    } else if (template.metric === "p99_latency") {
      currentValue = Math.round((template.threshold + Math.random() * 0.3) * 1000) / 1000;
    } else if (template.metric === "qps") {
      currentValue = Math.round(template.threshold * (1 + Math.random() * 0.3));
    }
  }

  return {
    id: `alert-${++mockAlertIdCounter}`,
    level: template.level,
    title: template.title,
    message: template.message,
    source: "backend",
    service,
    metric: template.metric,
    current_value: currentValue,
    threshold: template.threshold,
    status: "firing",
    firing_at: nowMs(),
  };
}

/* ===== 工具函数 ===== */

/** 格式化告警时间（相对时间） */
export function formatAlertTime(ts: number): string {
  const diff = nowMs() - ts;
  if (diff < 60 * 1000) return "刚刚";
  if (diff < 60 * 60 * 1000) return `${Math.floor(diff / 60 / 1000)} 分钟前`;
  if (diff < 24 * 60 * 60 * 1000) return `${Math.floor(diff / 60 / 60 / 1000)} 小时前`;
  return `${Math.floor(diff / 24 / 60 / 60 / 1000)} 天前`;
}

/** 格式化告警精确时间 HH:MM:SS */
export function formatAlertExactTime(ts: number): string {
  return formatAbsTime(ts);
}
