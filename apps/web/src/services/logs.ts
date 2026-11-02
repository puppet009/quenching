// 日志服务（T2-22 / T4-16）
// 对应 training-service 的日志查询接口（底层转查 Loki）。
//
// T4-16 改造：
//   - 全部接口收敛到统一请求层 http.ts（导出接口除外，需读取响应头）；
//   - sandbox_id 必填，级别走后端大写白名单（DEBUG/INFO/WARN/ERROR/FATAL）；
//   - 查询支持不透明游标 cursor / next_cursor 翻页；
//   - 实时日志用 /logs/tail 轮询（前端不上 SSE/WebSocket）；
//   - stream/start 回传后端钳制后的 interval_ms，42901 抛专用错误；
//   - export 读取 X-Log-Export-Truncated 头告知调用方结果是否被截断。

import axios, { AxiosError, AxiosResponse } from "axios";
import { ApiError, http, formatAbsTimeMs, formatDate, nowMs } from "@quenching/shared";
import { tokenStore } from "@quenching/shared";

/** 日志级别（前端四档；后端 FATAL 归并到 error） */
export type LogLevel = "debug" | "info" | "warn" | "error";

/** 单条日志 */
export interface LogItem {
  /** 时间戳（毫秒） */
  ts: number;
  /** 日志级别 */
  level: LogLevel;
  /** 服务名 */
  service: string;
  /** Pod 名（可选） */
  pod?: string;
  /** 日志内容 */
  message: string;
  /** 命名空间（可选） */
  namespace?: string;
}

/** 日志查询参数（对应 GET /logs/query） */
export interface LogQueryParams {
  /** 沙箱 ID（必填） */
  sandboxId: string;
  /** 服务名（可选，空=全部服务） */
  service?: string;
  /** 日志级别（可选，发送时转后端大写白名单） */
  level?: LogLevel;
  /** 关键词（可选） */
  keyword?: string;
  /** 开始时间（毫秒时间戳，可选，后端默认 1 小时前） */
  startTime?: number;
  /** 结束时间（毫秒时间戳，可选，后端默认当前） */
  endTime?: number;
  /** 返回条数上限（可选，后端默认 200、硬顶 1000） */
  limit?: number;
  /** 方向：backward 从新到旧（默认）/ forward 从旧到新 */
  direction?: "forward" | "backward";
  /** 翻页游标：取上一页响应中的 nextCursor 原样回传 */
  cursor?: string;
}

/** 日志查询结果 */
export interface LogQueryResult {
  /** 日志条目（已映射为前端 LogItem） */
  items: LogItem[];
  /** 本页实际返回条数 */
  total: number;
  /** 是否还有更多 */
  hasMore: boolean;
  /** 下一页游标（hasMore=true 时有值） */
  nextCursor: string;
  /** 本页条目涉及的服务列表 */
  services: string[];
}

/** 日志统计（对应 GET /logs/stats 的 data） */
export interface LogStats {
  totalCount: number;
  errorCount: number;
  warnCount: number;
  infoCount: number;
  byService: Record<string, number>;
  timeRange: string;
}

/** 实时流数量达到后端上限（HTTP 429 / code 42901）。 */
export class LogStreamLimitError extends Error {
  constructor() {
    super("实时日志流数量已达上限，请稍后再试");
    this.name = "LogStreamLimitError";
  }
}

/** 日志级别显示配置 */
export const LOG_LEVEL_CONFIG: Record<LogLevel, {
  label: string;
  textColor: string;
  bgColor: string;
  dotColor: string;
}> = {
  debug: {
    label: "DEBUG",
    textColor: "text-ink-500",
    bgColor: "bg-ink-100",
    dotColor: "bg-ink-400",
  },
  info: {
    label: "INFO",
    textColor: "text-brand-700",
    bgColor: "bg-brand-50",
    dotColor: "bg-brand-500",
  },
  warn: {
    label: "WARN",
    textColor: "text-warn-700",
    bgColor: "bg-warn-50",
    dotColor: "bg-warn-500",
  },
  error: {
    label: "ERROR",
    textColor: "text-danger-700",
    bgColor: "bg-danger-50",
    dotColor: "bg-danger-500",
  },
};

/* ===== 内部映射与参数拼装 ===== */

/** 后端 /logs/query 返回的单条原始结构。 */
interface RawLogEntry {
  timestamp: number;
  line: string;
  labels?: Record<string, string>;
}

/** 后端 /logs/query 返回的原始 data。 */
interface RawLogQueryResult {
  total: number;
  has_more: boolean;
  entries: RawLogEntry[];
  services?: string[];
  next_cursor?: string;
}

/** 后端级别（大写）映射为前端四档；FATAL 归并 error，未知归 info。 */
function mapLevel(raw?: string): LogLevel {
  switch ((raw || "").trim().toUpperCase()) {
    case "DEBUG":
      return "debug";
    case "WARN":
      return "warn";
    case "ERROR":
    case "FATAL":
      return "error";
    default:
      return "info";
  }
}

/** 前端级别转后端大写白名单值。 */
function toBackendLevel(level: LogLevel): string {
  return LOG_LEVEL_CONFIG[level].label; // DEBUG/INFO/WARN/ERROR
}

function mapEntry(raw: RawLogEntry): LogItem {
  const labels = raw.labels || {};
  const item: LogItem = {
    ts: raw.timestamp,
    level: mapLevel(labels.level),
    service: labels.service || "unknown",
    message: raw.line,
  };
  if (labels.pod) item.pod = labels.pod;
  if (labels.namespace) item.namespace = labels.namespace;
  return item;
}

function mapResult(raw: RawLogQueryResult): LogQueryResult {
  return {
    items: Array.isArray(raw.entries) ? raw.entries.map(mapEntry) : [],
    total: raw.total ?? 0,
    hasMore: raw.has_more === true,
    nextCursor: raw.next_cursor || "",
    services: Array.isArray(raw.services) ? raw.services : [],
  };
}

/** 拼装公共 query 参数（sandbox_id 必填）。 */
function buildSearch(params: {
  sandboxId: string;
  service?: string;
  level?: LogLevel;
  keyword?: string;
  startTime?: number;
  endTime?: number;
  limit?: number;
}): URLSearchParams {
  const search = new URLSearchParams();
  search.set("sandbox_id", params.sandboxId);
  if (params.service) search.set("service", params.service);
  if (params.level) search.set("level", toBackendLevel(params.level));
  if (params.keyword) search.set("keyword", params.keyword);
  if (typeof params.startTime === "number") search.set("start_time", String(params.startTime));
  if (typeof params.endTime === "number") search.set("end_time", String(params.endTime));
  if (typeof params.limit === "number" && params.limit > 0) search.set("limit", String(params.limit));
  return search;
}

/** 识别 HTTP 429 / code 42901（实时流数量上限）。 */
function isStreamLimitError(e: unknown): boolean {
  const ae = e as AxiosError<{ code?: number }>;
  if (ae?.response?.status === 429) return true;
  return ae?.response?.data?.code === 42901;
}

/* ===== 真实接口封装 ===== */

/**
 * 分页查询日志（GET /logs/query）。
 * 首页不传 cursor；向后翻页时把上一页 nextCursor 原样回传。
 */
export async function queryLogs(params: LogQueryParams): Promise<LogQueryResult> {
  const search = buildSearch(params);
  if (params.direction) search.set("direction", params.direction);
  if (params.cursor) search.set("cursor", params.cursor);
  const data = await http.get<unknown, RawLogQueryResult>(`/logs/query?${search.toString()}`);
  return mapResult(data);
}

/** 获取最新 N 条日志（GET /logs/latest），默认从新到旧，用于面板初始加载。 */
export async function getLatestLogs(
  sandboxId: string,
  opts: { service?: string; limit?: number } = {},
): Promise<LogQueryResult> {
  const search = buildSearch({ sandboxId, service: opts.service, limit: opts.limit });
  const data = await http.get<unknown, RawLogQueryResult>(`/logs/latest?${search.toString()}`);
  return mapResult(data);
}

/** 日志统计（GET /logs/stats）。 */
export async function getLogStats(
  sandboxId: string,
  opts: { startTime?: number; endTime?: number } = {},
): Promise<LogStats> {
  const search = buildSearch({ sandboxId, startTime: opts.startTime, endTime: opts.endTime });
  const data = await http.get<unknown, {
    total_count?: number;
    error_count?: number;
    warn_count?: number;
    info_count?: number;
    by_service?: Record<string, number>;
    time_range?: string;
  }>(`/logs/stats?${search.toString()}`);
  return {
    totalCount: data.total_count ?? 0,
    errorCount: data.error_count ?? 0,
    warnCount: data.warn_count ?? 0,
    infoCount: data.info_count ?? 0,
    byService: data.by_service || {},
    timeRange: data.time_range || "",
  };
}

/**
 * 轮询式实时日志（GET /logs/tail）。
 * 返回 since（毫秒）之后的新日志，从旧到新排列，前端直接追加。
 */
export async function tailLogs(
  sandboxId: string,
  since: number,
  opts: { service?: string; limit?: number } = {},
): Promise<LogQueryResult> {
  const search = buildSearch({ sandboxId, service: opts.service, limit: opts.limit });
  search.set("since", String(since));
  const data = await http.get<unknown, RawLogQueryResult>(`/logs/tail?${search.toString()}`);
  return mapResult(data);
}

/**
 * 可用服务列表（GET /logs/services）。
 * 走 Loki /series，返回时间窗口内有日志输出的全部服务（已按名称排序）。
 */
export async function listLogsServices(
  sandboxId: string,
  opts: { startTime?: number; endTime?: number } = {},
): Promise<string[]> {
  const search = buildSearch({ sandboxId, startTime: opts.startTime, endTime: opts.endTime });
  const data = await http.get<unknown, string[]>(`/logs/services?${search.toString()}`);
  return Array.isArray(data) ? data : [];
}

/** 启动实时日志流（POST /logviewer/stream/start）。 */
export async function startLogStream(req: {
  sandboxId: string;
  service?: string;
  /** 期望轮询间隔（毫秒），后端会钳制到允许下限；不传用后端默认值。 */
  intervalMs?: number;
}): Promise<{ started: boolean; intervalMs: number }> {
  try {
    const data = await http.post<unknown, { started?: boolean; interval_ms?: number }>(
      "/logviewer/stream/start",
      {
        sandbox_id: req.sandboxId,
        service: req.service || "",
        interval_ms: req.intervalMs ?? 0,
      },
    );
    return { started: data.started === true, intervalMs: data.interval_ms ?? 0 };
  } catch (e) {
    if (isStreamLimitError(e)) throw new LogStreamLimitError();
    throw e;
  }
}

/** 停止实时日志流（POST /logviewer/stream/stop）。 */
export async function stopLogStream(sandboxId: string): Promise<void> {
  await http.post("/logviewer/stream/stop", { sandbox_id: sandboxId });
}

/** 日志导出参数（GET /logviewer/export）。 */
export interface ExportLogsParams {
  sandboxId: string;
  service?: string;
  level?: LogLevel;
  keyword?: string;
  startTime?: number;
  endTime?: number;
}

/** 日志导出结果。 */
export interface ExportLogsResult {
  /** 日志文本文件内容 */
  blob: Blob;
  /** 后端指定的下载文件名 */
  filename: string;
  /** 是否命中导出上限被截断（响应头 X-Log-Export-Truncated=1） */
  truncated: boolean;
}

/** 从 Content-Disposition 中解析 filename。 */
function parseFilename(disposition?: string): string {
  if (!disposition) return "";
  // 优先 filename*=UTF-8''xx，其次 filename="xx"
  const utf8 = /filename\*=(?:UTF-8'')?([^;]+)/i.exec(disposition);
  if (utf8?.[1]) {
    try {
      return decodeURIComponent(utf8[1].trim().replace(/^"|"$/g, ""));
    } catch {
      // 解码失败时落回普通 filename
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(disposition);
  return plain?.[1]?.trim() || "";
}

/**
 * 导出日志为纯文本文件（GET /logviewer/export）。
 *
 * 该接口返回 text/plain 流并携带 X-Log-Export-Truncated / Content-Disposition
 * 响应头，统一请求层只解包 data 拿不到响应头，故这里用独立 axios 请求并
 * 手工注入与 http.ts 相同的 Authorization（不修改 http.ts）。
 */
export async function exportLogs(params: ExportLogsParams): Promise<ExportLogsResult> {
  const search = buildSearch(params);
  let resp: AxiosResponse<Blob>;
  try {
    resp = await axios.get(`/api/v1/logviewer/export?${search.toString()}`, {
      headers: {
        ...(tokenStore.get() ? { Authorization: `Bearer ${tokenStore.get()}` } : {}),
      },
      responseType: "blob",
      timeout: 30000,
    });
  } catch (e) {
    const ae = e as AxiosError<Blob>;
    if (ae.response?.data instanceof Blob) {
      // 错误体是 JSON 包在 Blob 里，尝试解析出后端 msg。
      const text = await ae.response.data.text().catch(() => "");
      try {
        const body = JSON.parse(text) as { code?: number; msg?: string };
        throw new ApiError(body.code ?? 50001, body.msg || "日志导出失败");
      } catch (parseErr) {
        if (parseErr instanceof ApiError) throw parseErr;
      }
    }
    throw new Error("日志导出失败，请稍后重试", { cause: e });
  }

  const truncated = resp.headers["x-log-export-truncated"] === "1";
  const filename = parseFilename(resp.headers["content-disposition"])
    || `logs-${params.sandboxId}-${nowMs()}.txt`;
  return { blob: resp.data, filename, truncated };
}

/* ====================================================================== */
/* 以下为演示页（/metrics-demo）专用 Mock 生成器，T4-16 起业务代码禁止引用。 */
/* ====================================================================== */

const MOCK_SERVICES = ["order-service", "product-service", "user-service", "payment-service"];

const MOCK_LOG_TEMPLATES: Array<{ level: LogLevel; message: string }> = [
  { level: "info", message: "Request processed successfully" },
  { level: "info", message: "User login successful, userId={uid}" },
  { level: "info", message: "Database connection pool initialized" },
  { level: "info", message: "Cache hit rate: {rate}%" },
  { level: "info", message: "Order {orderId} created successfully" },
  { level: "info", message: "Health check passed" },
  { level: "debug", message: "SQL query executed in {ms}ms" },
  { level: "debug", message: "Cache key {key} set with TTL 300s" },
  { level: "debug", message: "RPC call to {svc} completed" },
  { level: "warn", message: "Slow query detected: {ms}ms" },
  { level: "warn", message: "Memory usage approaching threshold: {pct}%" },
  { level: "warn", message: "Connection pool nearly exhausted, active={count}" },
  { level: "warn", message: "Retry attempt {n} for upstream service" },
  { level: "error", message: "Database connection failed: connection refused" },
  { level: "error", message: "NullPointerException at com.example.service.OrderService.process" },
  { level: "error", message: "Timeout waiting for response from payment-service" },
  { level: "error", message: "Failed to parse request body: invalid JSON format" },
  { level: "error", message: "Circuit breaker OPEN for downstream service" },
];

let mockLogTsCounter = nowMs();

/** 生成模拟日志数据（仅演示页使用） */
export function generateMockLogs(
  count = 50,
  options?: { service?: string; level?: LogLevel; keyword?: string },
): LogItem[] {
  const logs: LogItem[] = [];
  let ts = mockLogTsCounter;

  for (let i = 0; i < count; i++) {
    // 倒序生成：新的在前
    ts -= Math.floor(Math.random() * 3000) + 500; // 每条间隔 0.5-3.5 秒

    const template = MOCK_LOG_TEMPLATES[Math.floor(Math.random() * MOCK_LOG_TEMPLATES.length)];

    // 按级别过滤
    if (options?.level && template.level !== options.level) {
      i--;
      continue;
    }

    const service = options?.service
      || MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)];

    // 替换占位符
    const message = template.message
      .replace("{uid}", String(10000 + Math.floor(Math.random() * 90000)))
      .replace("{rate}", String(85 + Math.floor(Math.random() * 15)))
      .replace("{orderId}", `ORD${nowMs()}${Math.floor(Math.random() * 1000)}`)
      .replace("{ms}", String(50 + Math.floor(Math.random() * 450)))
      .replace("{key}", `user:session:${Math.random().toString(36).slice(2, 10)}`)
      .replace("{svc}", MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)])
      .replace("{pct}", String(75 + Math.floor(Math.random() * 20)))
      .replace("{count}", String(8 + Math.floor(Math.random() * 7)))
      .replace("{n}", String(1 + Math.floor(Math.random() * 3)));

    // 按关键字过滤
    if (options?.keyword && !message.toLowerCase().includes(options.keyword.toLowerCase())) {
      i--;
      continue;
    }

    const podIdx = Math.floor(Math.random() * 3) + 1;
    logs.push({
      ts,
      level: template.level,
      service,
      pod: `${service}-pod-${podIdx}`,
      message,
      namespace: "sandbox-12345",
    });
  }

  mockLogTsCounter = ts;
  return logs;
}

/** 生成新的模拟日志（用于实时刷新，仅演示页使用） */
export function generateMockNewLogs(count = 3, service?: string): LogItem[] {
  const now = nowMs();
  const logs: LogItem[] = [];

  for (let i = 0; i < count; i++) {
    const ts = now - i * 1000;
    const template = MOCK_LOG_TEMPLATES[Math.floor(Math.random() * MOCK_LOG_TEMPLATES.length)];
    const svc = service || MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)];

    const message = template.message
      .replace("{uid}", String(10000 + Math.floor(Math.random() * 90000)))
      .replace("{rate}", String(85 + Math.floor(Math.random() * 15)))
      .replace("{orderId}", `ORD${nowMs()}${Math.floor(Math.random() * 1000)}`)
      .replace("{ms}", String(50 + Math.floor(Math.random() * 450)))
      .replace("{key}", `user:session:${Math.random().toString(36).slice(2, 10)}`)
      .replace("{svc}", MOCK_SERVICES[Math.floor(Math.random() * MOCK_SERVICES.length)])
      .replace("{pct}", String(75 + Math.floor(Math.random() * 20)))
      .replace("{count}", String(8 + Math.floor(Math.random() * 7)))
      .replace("{n}", String(1 + Math.floor(Math.random() * 3)));

    const podIdx = Math.floor(Math.random() * 3) + 1;
    logs.push({
      ts,
      level: template.level,
      service: svc,
      pod: `${svc}-pod-${podIdx}`,
      message,
      namespace: "sandbox-12345",
    });
  }

  return logs;
}

/* ===== 工具函数 ===== */

/** 格式化日志时间戳 */
export function formatLogTime(ts: number): string {
  return formatAbsTimeMs(ts);
}

/** 格式化日志日期 */
export function formatLogDate(ts: number): string {
  return formatDate(ts);
}
