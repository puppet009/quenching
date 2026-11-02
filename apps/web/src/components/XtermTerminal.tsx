// xterm.js 终端组件（T1-21）
//
// 封装 xterm.js，接 WebSocket 渲染终端，适配面板尺寸。
//
// 注意：xterm.js 是可选依赖，未安装时自动降级为 DOM 渲染模式。
// 安装方式：npm install xterm xterm-addon-fit
//
// 功能：
//   - xterm.js 渲染（优先，高性能）
//   - WebSocket 连接（通过 useTerminalWS）
//   - 自适应面板尺寸（fit addon）
//   - 复制粘贴支持
//   - 主题配置（深色/浅色）
//   - 字体大小调整

import { useRef, useEffect, useState } from "react";
import "xterm/css/xterm.css";
import { useTerminalWS } from "../hooks/useTerminalWS";
import { feedTerminalInput } from "../utils/operationTaxonomy";

export interface XtermTerminalProps {
  /** WebSocket 地址 */
  wsUrl?: string;
  /** 鉴权 token */
  token?: string;
  /** 会话 ID */
  sessionId?: string;
  /** 目标 Pod/服务 */
  target?: string;
  /** 主题：dark / light */
  theme?: "dark" | "light";
  /** 字体大小 */
  fontSize?: number;
  /** 终端高度 */
  height?: string;
  /** 是否自动连接 */
  autoConnect?: boolean;
  /** 连接状态变化回调 */
  onStatusChange?: (status: string) => void;
  /** 用户在终端提交了一条完整命令（回车触发，已剥离控制字符） */
  onCommand?: (line: string) => void;
  /** 当前 shell 通过 OSC 7 报告的真实工作目录。 */
  onCwdChange?: (cwd: string) => void;
  className?: string;
}

/**
 * xterm.js 终端组件。
 *
 * 优先使用 xterm.js 渲染（需要安装依赖），未安装时显示提示。
 * 通过 WebSocket 与后端终端服务通信。
 */
export function XtermTerminal({
  wsUrl,
  token,
  sessionId,
  target,
  theme = "dark",
  fontSize = 15,
  height = "400px",
  autoConnect = true,
  onStatusChange,
  onCommand,
  onCwdChange,
  className = "",
}: XtermTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<import("xterm").Terminal | null>(null);
  const fitRef = useRef<() => void>(() => {});
  const lineBufferRef = useRef("");
  const onCommandRef = useRef(onCommand);
  onCommandRef.current = onCommand;
  const onCwdChangeRef = useRef(onCwdChange);
  onCwdChangeRef.current = onCwdChange;
  const [xtermAvailable, setXtermAvailable] = useState<boolean | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // WebSocket 连接管理
  const wsState = useTerminalWS({
    url: wsUrl || "",
    token,
    sessionId,
    target,
    autoConnect: false, // 等 xterm 初始化完再连
    onData: (data) => {
      // 将收到的数据写入终端
      if (terminalRef.current) {
        terminalRef.current.write(data);
      }
    },
    onConnected: (connId) => {
      console.log("[XtermTerminal] 已连接:", connId);
      onStatusChange?.("connected");
      fitRef.current();
    },
    onDisconnected: () => {
      onStatusChange?.("disconnected");
    },
  });
  const wsRef = useRef(wsState);
  wsRef.current = wsState;

  // 懒加载 xterm.js
  useEffect(() => {
    let mounted = true;
    const cleanups: Array<() => void> = [];

    async function initXterm() {
      try {
        // 动态导入 xterm.js
        const xtermModule = await import(/* webpackChunkName: "xterm" */ "xterm");
        const Terminal = xtermModule.Terminal;

        if (!mounted || !containerRef.current) return;

        const term = new Terminal({
          fontSize,
          fontFamily: '"Cascadia Code", Consolas, Menlo, monospace',
          lineHeight: 1.3,
          cursorBlink: true,
          theme: theme === "dark" ? darkTheme : lightTheme,
          convertEol: true,
          scrollback: 1000,
        });

        terminalRef.current = term;
        const cwdHandler = term.parser.registerOscHandler(7, (value: string) => {
          const cwd = parseTerminalCwd(value);
          if (cwd !== undefined) onCwdChangeRef.current?.(cwd);
          return true;
        });
        cleanups.push(() => cwdHandler.dispose());
        term.open(containerRef.current);

        // 统一收集本实例的清理函数，最终一次性返回（切勿在中途 return，
        // 否则会跳过后面的键盘接线与自动连接）。

        // 尝试加载 fit addon
        try {
          const fitModule = await import(/* webpackChunkName: "xterm-fit" */ "xterm-addon-fit");
          if (!mounted) return;
          const FitAddon = fitModule.FitAddon;
          const fitAddon = new FitAddon();
          term.loadAddon(fitAddon);
          let frame = 0;
          const fit = () => {
            cancelAnimationFrame(frame);
            frame = requestAnimationFrame(() => {
              const element = containerRef.current;
              if (!mounted || !element?.clientWidth || !element.clientHeight) return;
              try { fitAddon.fit(); wsRef.current.resize(term.cols, term.rows); } catch { /* hidden/disposed terminal */ }
            });
          };
          fitRef.current = fit;
          const observer = new ResizeObserver(fit);
          observer.observe(containerRef.current);
          window.addEventListener("resize", fit);
          void document.fonts.ready.then(() => { if (mounted) fit(); });
          fit();
          cleanups.push(() => {cancelAnimationFrame(frame);observer.disconnect();window.removeEventListener("resize", fit);fitRef.current = () => {};});
        } catch (e) {
          console.log("[XtermTerminal] xterm-addon-fit 未安装，跳过自适应");
        }

        // 用户输入 → WebSocket；同时本地重建命令行，供操作时间线留痕。
        if (!mounted) return;
        const disposable = term.onData((data: string) => {
          wsState.send(data);
          const next = feedTerminalInput(lineBufferRef.current, data);
          lineBufferRef.current = next.buffer;
          next.submitted.forEach((line) => onCommandRef.current?.(line));
        });
        cleanups.push(() => disposable.dispose());

        setXtermAvailable(true);
        setIsLoading(false);

        // 如果自动连接，xterm 就绪后连接
        if (autoConnect && wsUrl && token) {
          wsState.connect();
        }

      } catch (e) {
        console.warn("[XtermTerminal] xterm.js 未安装:", e);
        if (mounted) {
          setXtermAvailable(false);
          setIsLoading(false);
        }
      }
    }

    initXterm();

    return () => {
      mounted = false;
      cleanups.forEach((cleanup) => cleanup());
      if (terminalRef.current) {
        terminalRef.current.dispose();
        terminalRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (terminalRef.current) { terminalRef.current.options.fontSize = fontSize; fitRef.current(); }
  }, [fontSize, xtermAvailable]);

  // 状态变化通知
  useEffect(() => {
    onStatusChange?.(wsState.status);
  }, [wsState.status, onStatusChange]);

  // 连接状态徽章
  const statusBadge = {
    idle: { label: "未连接", color: "bg-ink-400" },
    connecting: { label: "连接中...", color: "bg-amber-500" },
    connected: { label: "已连接", color: "bg-emerald-500" },
    disconnected: { label: "已断开", color: "bg-red-500" },
    reconnecting: { label: `重连中(${wsState.reconnectCount})`, color: "bg-amber-500" },
    error: { label: "连接错误", color: "bg-red-500" },
  }[wsState.status] || { label: wsState.status, color: "bg-ink-400" };

  return (
    <div
      className={`flex flex-col rounded-xl overflow-hidden border border-ink-200 ${className}`}
      style={{ height }}
    >
      {/* 标题栏 */}
      <div
        className={`flex items-center justify-between px-3 py-2 ${
          theme === "dark" ? "bg-[#1e1e1e]" : "bg-ink-100"
        }`}
      >
        <div className="flex items-center gap-2">
          <div className="flex gap-1">
            <span className="w-3 h-3 rounded-full bg-[#ff5f56]" />
            <span className="w-3 h-3 rounded-full bg-[#ffbd2e]" />
            <span className="w-3 h-3 rounded-full bg-[#27c93f]" />
          </div>
          <span
            className={`text-[11px] ${
              theme === "dark" ? "text-ink-400" : "text-ink-500"
            } ml-2`}
          >
            {target || "terminal"} — bash
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="flex items-center gap-1.5">
            <span className={`w-2 h-2 rounded-full ${statusBadge.color} ${
              wsState.status === "connecting" || wsState.status === "reconnecting"
                ? "animate-pulse"
                : ""
            }`} />
            <span
              className={`text-[10px] ${
                theme === "dark" ? "text-ink-400" : "text-ink-500"
              }`}
            >
              {statusBadge.label}
            </span>
          </span>
          {(wsState.status === "error" || wsState.status === "disconnected") && wsUrl && xtermAvailable && (
            <button type="button" onClick={wsState.connect} className="rounded px-2 py-1 text-xs font-medium text-brand-400 hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-brand-400">
              重新连接
            </button>
          )}
        </div>
      </div>

      {wsState.error && (
        <div role="alert" className="border-b border-danger/20 bg-danger/10 px-3 py-3 text-sm leading-relaxed text-danger break-words">
          <p className="font-semibold">终端连接失败</p>
          <p>{wsState.error}</p>
        </div>
      )}

      {/* 终端容器 */}
      <div className="flex-1 relative overflow-hidden" style={{ background: theme === "dark" ? "#1e1e1e" : "#ffffff" }}>
        {/* 加载中 */}
        {isLoading && (
          <div className="absolute inset-0 flex items-center justify-center text-ink-400 text-[13px]">
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 border-2 border-ink-300 border-t-transparent rounded-full animate-spin" />
              正在加载终端...
            </div>
          </div>
        )}

        {/* xterm 不可用提示 */}
        {!isLoading && !xtermAvailable && (
          <div className="absolute inset-0 flex flex-col items-center justify-center text-ink-400 text-[13px] p-6">
            <div className="text-3xl mb-3">💻</div>
            <div className="font-medium text-ink-600 mb-2">xterm.js 未安装</div>
            <div className="text-[12px] text-center mb-4">
              请安装 xterm.js 以获得完整终端体验
            </div>
            <code className="text-[11px] bg-ink-100 px-3 py-1.5 rounded-lg">
              npm install xterm xterm-addon-fit
            </code>
            {wsUrl && (
              <button
                onClick={() => wsState.connect()}
                disabled={wsState.status === "connecting" || wsState.status === "connected"}
                className="mt-4 px-4 py-2 bg-brand-500 hover:bg-brand-600 disabled:bg-ink-300 text-white text-[12px] rounded-lg transition-colors"
              >
                {wsState.status === "connected" ? "已连接" : wsState.status === "connecting" ? "连接中..." : "连接终端"}
              </button>
            )}
          </div>
        )}

        {/* xterm 挂载点 */}
        <div
          ref={containerRef}
          className="w-full h-full"
          style={{ display: xtermAvailable ? "block" : "none" }}
        />
      </div>
    </div>
  );
}

/** OSC 7 uses a file URI; decode once so literal percent signs remain intact. */
export function parseTerminalCwd(value: string): string | undefined {
  // eslint-disable-next-line no-control-regex -- OSC paths must reject C0 control bytes.
  if (!value.startsWith("file://") || /[\x00-\x1f\x7f]/.test(value)) return;
  try {
    const uri = new URL(value);
    if (uri.protocol !== "file:" || uri.search || uri.hash) return;
    const cwd = decodeURIComponent(uri.pathname);
    // eslint-disable-next-line no-control-regex -- Percent-decoded C0 control bytes are also invalid.
    if (cwd.startsWith("/") && !/[\x00-\x1f\x7f]/.test(cwd)) return cwd;
  } catch {
    // Ignore malformed notifications; terminal output is still rendered normally.
  }
}

/* ===== 主题配置 ===== */

const darkTheme = {
  foreground: "#d4d4d4",
  background: "#1e1e1e",
  cursor: "#d4d4d4",
  cursorAccent: "#1e1e1e",
  selectionBackground: "#264f78",
  black: "#000000",
  red: "#cd3131",
  green: "#0dbc79",
  yellow: "#e5e510",
  blue: "#2472c8",
  magenta: "#bc3fbc",
  cyan: "#11a8cd",
  white: "#e5e5e5",
  brightBlack: "#666666",
  brightRed: "#f14c4c",
  brightGreen: "#23d18b",
  brightYellow: "#f5f543",
  brightBlue: "#3b8eea",
  brightMagenta: "#d670d6",
  brightCyan: "#29b8db",
  brightWhite: "#ffffff",
};

const lightTheme = {
  foreground: "#333333",
  background: "#ffffff",
  cursor: "#333333",
  cursorAccent: "#ffffff",
  selectionBackground: "#add6ff",
  black: "#000000",
  red: "#cd3131",
  green: "#005cc5",
  yellow: "#949800",
  blue: "#0451a5",
  magenta: "#bc05bc",
  cyan: "#0598bc",
  white: "#555555",
  brightBlack: "#666666",
  brightRed: "#cd3131",
  brightGreen: "#14ce14",
  brightYellow: "#b5ba00",
  brightBlue: "#0451a5",
  brightMagenta: "#bc05bc",
  brightCyan: "#0598bc",
  brightWhite: "#a5a5a5",
};

export default XtermTerminal;
