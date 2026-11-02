// 实训监控服务（第五片）
// 对应 training-service（经 api-gateway /api/v1）：
//   GET /metrics/query       指标区间查询（Prometheus）
//   GET /metrics/definitions 指标定义列表
//   GET /health/overview     沙箱整体健康概览（所有服务卡片）
//
// 说明：sandbox_id 统一传 query 中的沙箱标识（与日志面板一致），
// 由 training-service 内部映射到 K8s namespace。

import { http } from "@quenching/shared";
import type { DataPoint, MetricName } from "@quenching/shared";

/* ===== 指标 ===== */

/** GET /metrics/query 请求参数 */
export interface MetricQueryParams {
  /** 指标名：cpu_usage/memory_usage/qps/error_rate/p99_latency */
  metric: MetricName;
  service?: string;
  instance?: string;
  /** 起始时间（毫秒），缺省后端取最近 15 分钟 */
  start_time?: number;
  /** 结束时间（毫秒） */
  end_time?: number;
  /** 步长（秒），默认 15 */
  step?: number;
  /** 聚合方式 avg/max/min，默认 avg */
  aggregation?: "avg" | "max" | "min";
}

/** /metrics/query 响应（后端 QueryResult） */
export interface MetricQueryResult {
  metric: MetricName;
  unit: string;
  service: string;
  instance: string;
  points: DataPoint[];
}

/** /metrics/definitions 单条定义 */
export interface MetricDefinition {
  name: MetricName;
  display_name: string;
  unit: string;
  category: string;
}

/** 查询指标区间数据 */
export function queryMetricRange(params: MetricQueryParams): Promise<MetricQueryResult> {
  return http.get<unknown, MetricQueryResult>("/metrics/query", { params });
}

/** 指标定义列表 */
export function listMetricDefinitions(): Promise<MetricDefinition[]> {
  return http.get<unknown, MetricDefinition[]>("/metrics/definitions");
}

/* ===== 健康概览 ===== */

/** 单服务健康卡片（后端 ServiceCard） */
export interface HealthServiceCard {
  service: string;
  /** healthy/warning/critical/unknown */
  status: "healthy" | "warning" | "critical" | "unknown";
  cpu_usage: number;
  memory_usage: number;
  qps: number;
  error_rate: number;
  p99_latency: number;
  alert_count: number;
  critical_count: number;
  warning_count: number;
  reason?: string;
  updated_at: number;
}

/** /health/overview 响应（后端 DashboardOverview） */
export interface HealthOverview {
  sandbox_id: string;
  total_services: number;
  healthy_count: number;
  warning_count: number;
  critical_count: number;
  unknown_count: number;
  total_alerts: number;
  services: HealthServiceCard[];
  updated_at: number;
}

/** 获取沙箱整体健康概览（sandbox_id 必填） */
export function getHealthOverview(sandboxId: string): Promise<HealthOverview> {
  return http.get<unknown, HealthOverview>("/health/overview", {
    params: { sandbox_id: sandboxId },
  });
}
