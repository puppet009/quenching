import { nowMs, startTimeSync } from "../utils/time";

type ErrorKind = "error" | "rejection" | "react" | "request" | "resource" | "console";
interface BrowserReport {
  app: "web" | "admin";
  kind: ErrorKind;
  message: string;
  stack: string;
  path: string;
  client_time: number;
}

function diagnosticText(value: string, limit: number): string {
  return value.replace(/https?:\/\/[^\s)]+/g, raw => {
    try { const url = new URL(raw); return url.origin + url.pathname; } catch { return "[url]"; }
  }).replace(/\bBearer\s+[a-z0-9._~+/=-]+/gi, "Bearer [redacted]")
    .replace(/\b(token|access_token|refresh_token|authorization|password|secret|api[_-]?key)\s*[:=]\s*["']?[^,;\s"'&]+/gi, "$1=[redacted]")
    .slice(0, limit);
}

async function transport(report: BrowserReport): Promise<unknown> {
  return fetch("/api/v1/client/logs", {
    method: "POST", headers: {"Content-Type": "application/json"},
    body: JSON.stringify(report), keepalive: true, credentials: "omit",
    signal: AbortSignal.timeout(5000),
  });
}

export class BrowserTelemetry {
  private windowAt = -Infinity;
  private sent = 0;
  private seen = new Set<string>();

  constructor(
    private app: "web" | "admin",
    private send: (report: BrowserReport) => Promise<unknown> = transport,
    private monotonic = () => performance.now(),
    private pathname = () => window.location.pathname,
  ) {}

  capture(kind: ErrorKind, error: unknown, componentStack = ""): void {
    const now = this.monotonic();
    if (now - this.windowAt >= 60_000) { this.windowAt = now; this.sent = 0; this.seen.clear(); }
    if (this.sent >= 5) return;
    const message = diagnosticText(error instanceof Error ? error.message : typeof error === "string" ? error : "未提供异常详情", 1000) || "未知异常";
    const stack = diagnosticText((error instanceof Error ? error.stack ?? "" : "") + componentStack, 2000);
    const path = this.pathname().split(/[?#]/, 1)[0].slice(0, 250);
    const key = `${kind}/${path}/${message}/${stack}`;
    if (this.seen.has(key)) return;
    this.seen.add(key);
    this.sent++;
    // A rejected transport is intentionally not sent back through telemetry.
    try { void this.send({app: this.app, kind, message, stack, path, client_time: Math.round(nowMs())}).catch(() => {}); } catch { /* best effort */ }
  }
}

let active: BrowserTelemetry | undefined;
let nativeConsoleError: typeof console.error | undefined;

export function reportBrowserError(kind: ErrorKind, error: unknown): void {
  active?.capture(kind, error);
}

export function startBrowserRuntime(app: "web" | "admin") {
  if (!active) {
    active = new BrowserTelemetry(app);
    startTimeSync();
    const originalError = console.error.bind(console);
    nativeConsoleError = originalError;
    console.error = (...args: unknown[]) => {
      // Existing caught failures (including terminal WebSocket errors) use this
      // path. Never serialize arbitrary argument objects, HTTP bodies or headers.
      try { active?.capture("console", args.find(arg => arg instanceof Error) ?? args.find(arg => typeof arg === "string") ?? "控制台错误"); } catch { /* preserve console output */ }
      originalError(...args);
    };
    window.addEventListener("error", event => {
      if (event instanceof ErrorEvent) active?.capture("error", event.error ?? event.message);
      else active?.capture("resource", "静态资源加载失败");
    }, true);
    window.addEventListener("unhandledrejection", event => active?.capture("rejection", event.reason));
  }
  const onReactError = (error: unknown, info: {componentStack?: string | null}) => {
    active?.capture("react", error, info.componentStack ?? "");
    nativeConsoleError?.(error);
  };
  return {
    onCaughtError: onReactError,
    onUncaughtError: onReactError,
    onRecoverableError: onReactError,
  };
}
