// 统一时间戳工具（T1-24）
//
// 规范：
//   - 全链路使用 UTC 毫秒时间戳（Unix timestamp in milliseconds）
//   - 会话基准时间：startedAt（训练开始时间戳）
//   - 相对时间：相对于 startedAt 的毫秒偏移
//   - 展示格式：
//     * 绝对时间：HH:MM:SS（日志、监控）
//     * 相对时间：mm:ss 或 hh:mm:ss（时间线、耗时）
//     * 复盘展示："第X分XX秒" 或 "X分Y秒"
//
// 实际时刻保持 Unix 毫秒值；显示使用页面加载时浏览器的 IANA 时区。
// nowMs 在成功校时后使用服务器时刻和单调时钟，避免用户电脑改时间影响查询窗口。

export const DISPLAY_TIME_ZONE = new Intl.DateTimeFormat().resolvedOptions().timeZone;
const clockFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: DISPLAY_TIME_ZONE, hourCycle: "h23", hour: "2-digit", minute: "2-digit", second: "2-digit",
});
const dateFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: DISPLAY_TIME_ZONE, year: "numeric", month: "2-digit", day: "2-digit",
});
const zoneFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: DISPLAY_TIME_ZONE, timeZoneName: "longOffset",
});

/** Include the event's UTC offset to distinguish repeated daylight-saving hours. */
export function formatTimeZone(tsMs: number): string {
  if (!Number.isFinite(tsMs)) return DISPLAY_TIME_ZONE;
  const offset = zoneFormatter.formatToParts(tsMs).find(part => part.type === "timeZoneName")?.value.replace(/^GMT/, "UTC");
  return offset && offset !== DISPLAY_TIME_ZONE ? `${DISPLAY_TIME_ZONE} · ${offset}` : DISPLAY_TIME_ZONE;
}

export class ServerClock {
  private serverAtArrival?: number;
  private arrivedAt = 0;

  synchronize(serverMs: number, sentAt: number, receivedAt: number): boolean {
    const roundTrip = receivedAt - sentAt;
    if (!Number.isFinite(serverMs) || serverMs <= 0 || !Number.isFinite(roundTrip) || roundTrip < 0 || roundTrip > 5000) return false;
    this.serverAtArrival = serverMs + roundTrip / 2;
    this.arrivedAt = receivedAt;
    return true;
  }

  now(monotonic = performance.now()): number {
    return this.serverAtArrival === undefined ? Date.now() : this.serverAtArrival + Math.max(0, monotonic - this.arrivedAt);
  }
}

const serverClock = new ServerClock();
let clockVersion = 0;
const clockListeners = new Set<() => void>();
export const serverClockVersion = () => clockVersion;
export function subscribeServerClock(listener: () => void): () => void {
  clockListeners.add(listener);
  return () => { clockListeners.delete(listener); };
}

export async function syncServerTime(fetcher: typeof fetch = fetch): Promise<boolean> {
  const sentAt = performance.now();
  try {
    const response = await fetcher("/api/v1/client/time", {cache: "no-store", signal: AbortSignal.timeout(5000)});
    if (!response.ok) return false;
    const body = await response.json();
    if (!serverClock.synchronize(body?.data?.unix_ms, sentAt, performance.now())) return false;
    clockVersion++;
    clockListeners.forEach(listener => listener());
    return true;
  } catch {
    return false;
  }
}

export function startTimeSync(): () => void {
  void syncServerTime();
  const timer = window.setInterval(() => { if (!document.hidden) void syncServerTime(); }, 60_000);
  const onVisible = () => { if (!document.hidden) void syncServerTime(); };
  document.addEventListener("visibilitychange", onVisible);
  return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", onVisible); };
}

/**
 * 获取当前 UTC 毫秒时间戳。
 * 统一入口，便于后续 mock 或校准。
 */
export function nowMs(): number {
  return Math.floor(serverClock.now());
}

/**
 * 格式化绝对时间戳为 HH:MM:SS。
 * 用于日志、监控等展示。
 */
export function formatAbsTime(tsMs: number): string {
  return Number.isFinite(tsMs) ? clockFormatter.format(tsMs) : "—";
}

/**
 * 格式化绝对时间戳为 HH:MM:SS.sss（毫秒精度）。
 * 用于日志时间戳、精确时间展示。
 */
export function formatAbsTimeMs(tsMs: number): string {
  const d = new Date(tsMs);
  const ms = d.getUTCMilliseconds().toString().padStart(3, "0");
  return `${formatAbsTime(tsMs)}.${ms}`;
}

/**
 * 格式化绝对时间戳为 YYYY-MM-DD HH:MM:SS。
 */
export function formatAbsDateTime(tsMs: number): string {
  if (!Number.isFinite(tsMs)) return "—";
  return `${formatDate(tsMs)} ${formatAbsTime(tsMs)}`;
}

export function formatDate(tsMs: number): string {
  if (!Number.isFinite(tsMs)) return "—";
  const parts = Object.fromEntries(dateFormatter.formatToParts(tsMs).map(p => [p.type, p.value]));
  return `${parts.year}-${parts.month}-${parts.day}`;
}

export function formatIsoMinute(iso: string | null | undefined): string {
  if (!iso) return "—";
  const at = Date.parse(iso);
  return Number.isFinite(at) ? formatAbsDateTime(at).slice(0, 16) : iso;
}

export function sessionElapsedMs(startedAt: string | null | undefined, at = nowMs()): number {
  const start = startedAt ? Date.parse(startedAt) : NaN;
  return Number.isFinite(start) ? Math.max(0, at - start) : 0;
}

/**
 * 将毫秒转换为相对时间字符串 mm:ss。
 * 不足 1 小时时使用此格式。
 */
export function formatRelTime(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  return `${min.toString().padStart(2, "0")}:${sec.toString().padStart(2, "0")}`;
}

/**
 * 将毫秒转换为相对时间字符串 hh:mm:ss。
 * 超过 1 小时自动补小时位。
 */
export function formatRelTimeLong(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSec / 3600);
  const min = Math.floor((totalSec % 3600) / 60);
  const sec = totalSec % 60;

  if (hours === 0) {
    return formatRelTime(ms);
  }

  return (
    `${hours.toString().padStart(2, "0")}:` +
    `${min.toString().padStart(2, "0")}:` +
    `${sec.toString().padStart(2, "0")}`
  );
}

/**
 * 格式化相对时间为人类可读的中文表达。
 *
 * 示例：
 *   3000ms → "3 秒"
 *   65000ms → "1 分 5 秒"
 *   3661000ms → "1 小时 1 分 1 秒"
 */
export function formatRelTimeChinese(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSec / 3600);
  const min = Math.floor((totalSec % 3600) / 60);
  const sec = totalSec % 60;

  const parts: string[] = [];
  if (hours > 0) parts.push(`${hours} 小时`);
  if (min > 0) parts.push(`${min} 分`);
  if (sec > 0 || parts.length === 0) parts.push(`${sec} 秒`);

  return parts.join(" ");
}

/**
 * 复盘时间线格式："第X分XX秒"。
 *
 * 示例：
 *   65000ms → "第1分05秒"
 *   3661000ms → "第1小时1分01秒"
 */
export function formatTimelineLabel(ms: number): string {
  const totalSec = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(totalSec / 3600);
  const min = Math.floor((totalSec % 3600) / 60);
  const sec = totalSec % 60;

  if (hours > 0) {
    return `第${hours}小时${min}分${sec.toString().padStart(2, "0")}秒`;
  }
  return `第${min}分${sec.toString().padStart(2, "0")}秒`;
}

/**
 * 计算相对时间（给定时间戳相对于基准时间）。
 *
 * @param tsMs 时间戳（毫秒）
 * @param baseMs 基准时间戳（毫秒），如会话 startedAt
 * @returns 相对时间偏移（毫秒），可能为负（如果 tsMs 在基准之前）
 */
export function toRelativeMs(tsMs: number, baseMs: number): number {
  return tsMs - baseMs;
}

/**
 * 将相对时间转换回绝对时间戳。
 *
 * @param relMs 相对时间偏移（毫秒）
 * @param baseMs 基准时间戳（毫秒）
 * @returns 绝对时间戳（毫秒）
 */
export function fromRelativeMs(relMs: number, baseMs: number): number {
  return baseMs + relMs;
}

/**
 * 相对时间 → "刚刚 / X秒前 / X分前 / X小时前 / X天前"。
 * 用于告警、消息等的相对时间展示。
 */
export function formatTimeAgo(tsMs: number, now: number = nowMs()): string {
  const diffMs = now - tsMs;
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 1) return "刚刚";
  if (diffSec < 60) return `${diffSec} 秒前`;

  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin} 分钟前`;

  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;

  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 30) return `${diffDay} 天前`;

  const diffMonth = Math.floor(diffDay / 30);
  if (diffMonth < 12) return `${diffMonth} 个月前`;

  const diffYear = Math.floor(diffMonth / 12);
  return `${diffYear} 年前`;
}

/**
 * 格式化带时区的 ISO 日期字符串为浏览器当地时间。
 * 用于后端返回的 created_at / updated_at 等字段。
 */
export function formatIsoDate(isoString: string): string {
  const d = new Date(isoString);
  if (isNaN(d.getTime())) return isoString;
  return formatAbsDateTime(d.getTime());
}

/**
 * 睡眠函数（异步等待）。
 */
export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * 会话时间上下文。
 * 用于在整个会话中统一基准时间，
 * 确保各组件展示的相对时间一致。
 */
export class SessionTimeContext {
  /** 会话开始时间戳（毫秒） */
  readonly startedAt: number;

  constructor(startedAt?: number) {
    this.startedAt = startedAt ?? nowMs();
  }

  /** 获取当前相对时间（毫秒） */
  nowRel(): number {
    return nowMs() - this.startedAt;
  }

  /** 将绝对时间戳转为相对时间 */
  toRelative(tsMs: number): number {
    return toRelativeMs(tsMs, this.startedAt);
  }

  /** 将相对时间转为绝对时间戳 */
  toAbsolute(relMs: number): number {
    return fromRelativeMs(relMs, this.startedAt);
  }

  /** 格式化相对时间为 mm:ss */
  formatRel(relMs: number): string {
    return formatRelTime(relMs);
  }

  /** 格式化相对时间为中文 */
  formatRelChinese(relMs: number): string {
    return formatRelTimeChinese(relMs);
  }

  /** 时间线标签格式 */
  formatTimeline(relMs: number): string {
    return formatTimelineLabel(relMs);
  }

  /** 已用时间 */
  elapsed(): number {
    return this.nowRel();
  }

  /** 已用时间格式化 */
  elapsedFormatted(): string {
    return formatRelTimeLong(this.nowRel());
  }
}
