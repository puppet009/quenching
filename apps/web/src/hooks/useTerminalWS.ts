// WebSocket 终端连接管理 Hook（T1-18 + T1-21）
//
// 封装 WS 终端连接的建立、鉴权、心跳保活、断线重连逻辑。
// 配合 XtermTerminal 组件使用。
//
// 连接流程：
//   1. 建立 WebSocket 连接（带 token 鉴权）
//   2. 收到 hello 事件，获取 connection_id
//   3. 启动心跳（30s ping / pong）
//   4. 收到 data 事件，渲染到终端
//   5. 用户输入通过 send 发送
//   6. 断线时自动重连（指数退避，最多 30s）
//
// 事件协议：
//   server -> client: hello / data / ping / pong / error / close
//   client -> server: data / pong / ping / resize

import { useState, useEffect, useRef, useCallback } from "react";

export interface TerminalConnectionState {
  /** 连接状态 */
  status: "idle" | "connecting" | "connected" | "disconnected" | "reconnecting" | "error";
  /** 连接 ID（连接成功后获得） */
  connectionId?: string;
  /** 错误信息 */
  error?: string;
  /** 重连次数 */
  reconnectCount: number;
}

export interface UseTerminalWSProps {
  /** WebSocket 地址 */
  url: string;
  /** 鉴权 token */
  token?: string;
  /** 会话 ID（训练 ID） */
  sessionId?: string;
  /** 目标 Pod/服务名 */
  target?: string;
  /** 是否自动连接 */
  autoConnect?: boolean;
  /** 收到数据回调 */
  onData?: (data: string) => void;
  /** 连接成功回调 */
  onConnected?: (connectionId: string) => void;
  /** 连接断开回调 */
  onDisconnected?: () => void;
}

export interface UseTerminalWSResult extends TerminalConnectionState {
  /** 发送数据 */
  send: (data: string) => void;
  /** 手动连接 */
  connect: () => void;
  /** 手动断开 */
  disconnect: () => void;
  /** 调整终端大小 */
  resize: (cols: number, rows: number) => void;
}

/** 心跳间隔（毫秒） */
const HEARTBEAT_INTERVAL = 30000;
/** 心跳超时（毫秒） */
const HEARTBEAT_TIMEOUT = 60000;
/** 最大重连间隔（毫秒） */
const MAX_RECONNECT_DELAY = 30000;
/** 初始重连延迟（毫秒） */
const INITIAL_RECONNECT_DELAY = 1000;

/**
 * WebSocket 终端连接 Hook。
 *
 * 负责：WS 连接建立、token 鉴权、心跳保活、断线重连、消息收发。
 */
export function useTerminalWS({
  url,
  token,
  sessionId,
  target,
  autoConnect = true,
  onData,
  onConnected,
  onDisconnected,
}: UseTerminalWSProps): UseTerminalWSResult {
  const [state, setState] = useState<TerminalConnectionState>({
    status: "idle",
    reconnectCount: 0,
  });

  const wsRef = useRef<WebSocket | null>(null);
  const heartbeatTimer = useRef<number | null>(null);
  const reconnectTimer = useRef<number | null>(null);
  const reconnectCountRef = useRef(0);
  const lastPongRef = useRef(0);
  const manualDisconnect = useRef(false);

  /** 清除心跳定时器 */
  const clearHeartbeat = useCallback(() => {
    if (heartbeatTimer.current) {
      clearInterval(heartbeatTimer.current);
      heartbeatTimer.current = null;
    }
  }, []);

  /** 清除重连定时器 */
  const clearReconnect = useCallback(() => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current);
      reconnectTimer.current = null;
    }
  }, []);

  /** 启动心跳 */
  const startHeartbeat = useCallback((ws: WebSocket) => {
    clearHeartbeat();
    lastPongRef.current = Date.now();

    heartbeatTimer.current = window.setInterval(() => {
      if (ws.readyState !== WebSocket.OPEN) return;

      // 检查是否超时
      if (Date.now() - lastPongRef.current > HEARTBEAT_TIMEOUT) {
        console.warn("[TerminalWS] 心跳超时，关闭连接");
        ws.close();
        return;
      }

      // 发送 ping
      try {
        ws.send(JSON.stringify({ type: "ping", timestamp: Date.now() }));
      } catch (e) {
        console.warn("[TerminalWS] 发送 ping 失败:", e);
      }
    }, HEARTBEAT_INTERVAL);
  }, [clearHeartbeat]);

  /** 计划重连 */
  const scheduleReconnect = useCallback(() => {
    clearReconnect();

    if (manualDisconnect.current) return;

    reconnectCountRef.current += 1;
    const delay = Math.min(
      INITIAL_RECONNECT_DELAY * Math.pow(2, reconnectCountRef.current - 1),
      MAX_RECONNECT_DELAY,
    );

    setState((prev) => ({
      ...prev,
      status: "reconnecting",
      reconnectCount: reconnectCountRef.current,
    }));

    console.log(`[TerminalWS] ${delay / 1000}s 后第 ${reconnectCountRef.current} 次重连...`);

    reconnectTimer.current = window.setTimeout(() => {
      connectWS();
    }, delay);
  }, [clearReconnect]);

  /** 建立 WebSocket 连接 */
  const connectWS = useCallback(() => {
    if (wsRef.current && (wsRef.current.readyState === WebSocket.OPEN || wsRef.current.readyState === WebSocket.CONNECTING)) {
      return;
    }

    setState((prev) => ({ ...prev, status: "connecting", error: undefined }));

    try {
      // 构建带鉴权参数的 URL；若传入地址已含查询串（buildExecWSURL 已带
      // target/token/container），用 & 追加，且仅在确有额外参数时追加。
      const params = new URLSearchParams();
      if (token) params.set("token", token);
      if (sessionId) params.set("session_id", sessionId);
      if (target) params.set("target", target);

      const extra = params.toString();
      let wsUrl: string;
      if (!extra) {
        wsUrl = url;
      } else if (url.includes("?")) {
        wsUrl = `${url}&${extra}`;
      } else {
        wsUrl = `${url}?${extra}`;
      }
      const ws = new WebSocket(wsUrl);
      wsRef.current = ws;

      ws.onopen = () => {
        if (wsRef.current !== ws) return;
        console.log("[TerminalWS] 连接已建立");
        // 等待 hello 事件才算真正连接成功
      };

      ws.onmessage = (event) => {
        if (wsRef.current !== ws) return;
        try {
          const msg = JSON.parse(event.data);

          switch (msg.type) {
            case "hello":
              setState((prev) => ({
                ...prev,
                status: "connected",
                connectionId: msg.data?.connection_id,
                reconnectCount: 0,
              }));
              reconnectCountRef.current = 0;
              startHeartbeat(ws);
              onConnected?.(msg.data?.connection_id);
              break;

            case "data":
              onData?.(msg.data);
              break;

            case "pong":
              lastPongRef.current = Date.now();
              break;

            case "ping":
              // 服务端 ping，回 pong
              ws.send(JSON.stringify({ type: "pong", timestamp: Date.now() }));
              break;

            case "error":
              // A rejected terminal session needs a manual retry after the
              // cause is resolved; automatic retries would hide the error.
              manualDisconnect.current = true;
              clearHeartbeat();
              clearReconnect();
              setState((prev) => ({
                ...prev,
                status: "error",
                error: msg.message || "终端错误",
              }));
              ws.close(1000, "terminal failed");
              break;

            case "close":
              manualDisconnect.current = true;
              clearHeartbeat();
              clearReconnect();
              setState((prev) => ({ ...prev, status: "disconnected", error: undefined }));
              ws.close(1000, "shell completed");
              break;

            default:
              // 未知类型，忽略
              break;
          }
        } catch (e) {
          // 非 JSON 消息，直接当数据处理
          onData?.(event.data);
        }
      };

      ws.onerror = (e) => {
        if (wsRef.current !== ws || manualDisconnect.current) return;
        console.error("[TerminalWS] 连接错误:", e);
        setState((prev) => ({
          ...prev,
          status: "error",
          error: prev.error || "连接失败",
        }));
      };

      ws.onclose = (event) => {
        if (wsRef.current !== ws) return;
        console.log(`[TerminalWS] 连接关闭 (code=${event.code})`);
        clearHeartbeat();
        wsRef.current = null;

        setState((prev) => ({
          ...prev,
          status: manualDisconnect.current && prev.status === "error" ? "error" : "disconnected",
        }));

        onDisconnected?.();

        // 非主动断开则尝试重连
        if (!manualDisconnect.current && event.code !== 1000) {
          scheduleReconnect();
        }
      };
    } catch (e) {
      console.error("[TerminalWS] 创建连接失败:", e);
      setState((prev) => ({
        ...prev,
        status: "error",
        error: (e as Error).message,
      }));
      scheduleReconnect();
    }
  }, [url, token, sessionId, target, onData, onConnected, onDisconnected, startHeartbeat, scheduleReconnect, clearHeartbeat, clearReconnect]);

  /** 发送数据 */
  const send = useCallback((data: string) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.warn("[TerminalWS] 连接未就绪，无法发送");
      return;
    }

    try {
      ws.send(JSON.stringify({ type: "data", data }));
    } catch (e) {
      console.error("[TerminalWS] 发送失败:", e);
    }
  }, []);

  /** 调整终端大小 */
  const resize = useCallback((cols: number, rows: number) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;

    try {
      ws.send(JSON.stringify({ type: "resize", cols, rows }));
    } catch (e) {
      console.error("[TerminalWS] 发送 resize 失败:", e);
    }
  }, []);

  /** 手动连接 */
  const connect = useCallback(() => {
    manualDisconnect.current = false;
    reconnectCountRef.current = 0;
    clearReconnect();
    connectWS();
  }, [connectWS, clearReconnect]);

  /** 手动断开 */
  const disconnect = useCallback(() => {
    manualDisconnect.current = true;
    clearHeartbeat();
    clearReconnect();

    const ws = wsRef.current;
    if (ws) {
      ws.close(1000, "manual disconnect");
    }
  }, [clearHeartbeat, clearReconnect]);

  // 自动连接
  useEffect(() => {
    if (autoConnect && token) {
      connect();
    }
    return () => {
      disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return {
    ...state,
    send,
    connect,
    disconnect,
    resize,
  };
}
