// AI 对话相关接口（对接 training-service）。
// 采用 SSE 流式接收：用 fetch + ReadableStream 逐帧解析。
import { http, tokenStore } from "@quenching/shared";

/** 消息角色 */
export type MessageRole = "user" | "assistant" | "system";

/** 对话类型 */
export type ConversationType =
  | "training_assistant"
  | "review_explainer"
  | "knowledge_qa"
  | "architecture_advisor";

/** 坑点类型 */
export type PitfallType = "causality_misjudgment" | "info_missing" | "overconfidence";

/** 置信度等级 */
export type ConfidenceLevel = "low" | "medium" | "high";

/** SSE 流式事件类型 */
export type StreamEventType =
  | "thinking"
  | "delta"
  | "done"
  | "error"
  | "tool_call"
  | "step";

/** 步骤时间线单项（过程透明，对应后端 StepEvent） */
export interface StepItem {
  stepId: string;
  state: string; // started/progress/completed/failed/awaiting_approval/rejected
  tool?: string;
  display?: string;
  summary?: string;
  detailJson?: string;
  /** 仅 awaiting_approval 携带：确认弹窗据此回调审批端点。 */
  approvalId?: string;
}

/** SSE 流式事件（与后端 StreamEvent 对齐） */
export interface StreamEvent {
  type: StreamEventType;
  delta?: string;
  is_pitfall?: boolean;
  pitfall_config_id?: number;
  pitfall_type?: PitfallType;
  confidence_level?: ConfidenceLevel;
  conversation_id?: number;
  message_id?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  cost?: number;
  error?: string;
  // 步骤时间线事件。
  step?: {
    step_id?: string;
    state?: string;
    tool?: string;
    display?: string;
    summary?: string;
    detail_json?: string;
    approval_id?: string;
  };
}

/** 创建对话请求 */
export interface CreateConversationReq {
  session_id?: number;
  conversation_type?: ConversationType;
}

/** 创建对话响应 */
export interface CreateConversationResp {
  conversation_id: number;
}

/** 前端消息模型（用于 UI 渲染） */
export interface ChatMessage {
  id: number | string; // 临时消息用 string（如 "tmp-xxx"）
  role: MessageRole;
  content: string;
  isPitfall?: boolean;
  pitfallType?: PitfallType;
  confidenceLevel?: ConfidenceLevel;
  isStreaming?: boolean; // AI 正在输出中
  isError?: boolean;
  errorMsg?: string;
  steps?: StepItem[]; // 本轮工具执行步骤（过程透明）
  createdAt?: string;
}

/** 创建对话 */
export function createConversation(params?: CreateConversationReq) {
  return http.post<unknown, CreateConversationResp>("/ai/conversations", params ?? {});
}

/**
 * 投递工具操作审批决定（M4）。
 * @param conversationId 对话 ID
 * @param approvalId 步骤事件下发的审批令牌
 * @param approved 是否同意
 */
export function resolveApproval(
  conversationId: number,
  approvalId: string,
  approved: boolean,
) {
  return http.post<unknown, { approval_id: string; approved: boolean }>(
    `/ai/conversations/${conversationId}/approvals`,
    { approval_id: approvalId, approved },
  );
}

/**
 * 发送消息并以 SSE 流式接收回复。
 * 返回一个 Promise，以及 onDelta / onDone / onError 回调。
 *
 * @param conversationId 对话 ID
 * @param message 用户消息内容
 * @param callbacks 流式回调
 * @returns AbortController 用于取消请求
 */
export function sendMessageStream(
  conversationId: number,
  message: string,
  callbacks: {
    onDelta?: (delta: string, event: StreamEvent) => void;
    onDone?: (event: StreamEvent) => void;
    onError?: (error: string) => void;
    onThinking?: () => void;
    onStep?: (step: StepItem) => void;
  },
): AbortController {
  const controller = new AbortController();

  (async () => {
    try {
      const token = tokenStore.get();
      const resp = await fetch(`/api/v1/ai/conversations/${conversationId}/messages/stream`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ message }),
        signal: controller.signal,
      });

      if (!resp.ok) {
        const errText = await resp.text().catch(() => "");
        callbacks.onError?.(`请求失败（${resp.status}）${errText ? `：${errText}` : ""}`);
        return;
      }

      const reader = resp.body?.getReader();
      if (!reader) {
        callbacks.onError?.("浏览器不支持流式响应");
        return;
      }

      const decoder = new TextDecoder("utf-8");
      let buffer = "";
      let terminal = false;

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // 解析 SSE 帧：data: {...}\n\n
        const lines = buffer.split("\n\n");
        buffer = lines.pop() ?? ""; // 最后一个可能是不完整的帧

        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed.startsWith("data:")) continue;

          const jsonStr = trimmed.slice(5).trim();
          if (!jsonStr) continue;

          try {
            const event: StreamEvent = JSON.parse(jsonStr);
            if (!terminal) handleStreamEvent(event, callbacks);
            if(event.type === "done" || event.type === "error") terminal=true;
          } catch {
            // 解析失败跳过，不中断流
          }
        }
      }
      if(!terminal && !controller.signal.aborted) callbacks.onError?.("回复中断，已保存的记录仍可在重新进入班次后恢复");
    } catch (err: unknown) {
      if ((err as { name?: string })?.name === "AbortError") {
        return; // 用户主动取消
      }
      callbacks.onError?.((err as Error)?.message ?? "网络错误");
    }
  })();

  return controller;
}

/** 处理单个 SSE 事件 */
function handleStreamEvent(
  event: StreamEvent,
  callbacks: {
    onDelta?: (delta: string, event: StreamEvent) => void;
    onDone?: (event: StreamEvent) => void;
    onError?: (error: string) => void;
    onThinking?: () => void;
    onStep?: (step: StepItem) => void;
  },
) {
  switch (event.type) {
    case "thinking":
      callbacks.onThinking?.();
      break;
    case "delta":
      if (event.delta) {
        callbacks.onDelta?.(event.delta, event);
      }
      break;
    case "done":
      callbacks.onDone?.(event);
      break;
    case "error":
      callbacks.onError?.(event.error ?? "AI 响应错误");
      break;
    case "step":
      if (event.step) {
        callbacks.onStep?.({
          stepId: event.step.step_id ?? "",
          state: event.step.state ?? "",
          tool: event.step.tool,
          display: event.step.display,
          summary: event.step.summary,
          detailJson: event.step.detail_json,
          approvalId: event.step.approval_id,
        });
      }
      break;
  }
}

/** 置信度显示名 */
export const CONFIDENCE_LABELS: Record<ConfidenceLevel, string> = {
  low: "低置信度",
  medium: "中等置信度",
  high: "高置信度",
};

/** 坑点类型显示名 */
export const PITFALL_TYPE_LABELS: Record<PitfallType, string> = {
  causality_misjudgment: "因果误判",
  info_missing: "信息遗漏",
  overconfidence: "过度自信",
};

export interface SessionHistoryMessage {
 id:number; role:MessageRole; content:string; created_at?:string;
 is_pitfall?:boolean; pitfall_type?:PitfallType; confidence_level?:ConfidenceLevel; hint_level?:number;
}
export interface SessionHistoryPage {messages:SessionHistoryMessage[];next_before_id:number;has_more:boolean}
export interface SessionConversationRestore extends SessionHistoryPage {session_id:number;conversation_id:number;session_status:string}
export function restoreSessionConversation(sessionId:number,signal?:AbortSignal){
 return http.get<unknown,SessionConversationRestore>(`/ai/sessions/${sessionId}/conversation`,{signal});
}
export function fetchSessionConversationHistory(sessionId:number,beforeId:number,signal?:AbortSignal){
 return http.get<unknown,SessionHistoryPage>(`/ai/sessions/${sessionId}/conversation/messages`,{params:{before_id:beforeId,limit:50},signal});
}
