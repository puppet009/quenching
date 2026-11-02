import { OperationTimeline } from "./training/OperationTimeline";
import { useEffect, useRef, useState } from "react";
import {useSessionConversation} from "../hooks/useSessionConversation";
import {mergeSessionMessages} from "../services/sessionConversation";
import { MessageBubble } from "./MessageBubble";
import { ApprovalModal } from "./ApprovalModal";
import {
  type ChatMessage,
  type StreamEvent,
  type StepItem,
  createConversation,
  resolveApproval,
  sendMessageStream,
  type ConversationType,
} from "../services/chat";

export interface ChatWindowProps {
  /** 会话 ID（训练场景传入） */
  sessionId?: number;
  /** 对话类型 */
  conversationType?: ConversationType;
  /** 对话 ID（已有对话时传入，不传则自动创建） */
  conversationId?: number;
  /** 初始消息列表 */
  initialMessages?: ChatMessage[];
  /** 占位文案 */
  placeholder?: string;
  /** 对话创建成功回调 */
  onConversationCreated?: (conversationId: number) => void;
  /** 消息发送完成回调 */
  onMessageDone?: (event: StreamEvent) => void;
  /** 用户发出提问（操作时间线留痕用）。 */
  onUserQuestion?: (content: string) => void;
  /** 审批决定已提交（操作时间线留痕用）。 */
  onApprovalResolved?: (approved: boolean) => void;
  /** 高度（如 400px / 100%） */
  height?: string;
  /** 自定义类名 */
  className?: string;
}

/**
 * 对话窗口组件（T2-28）
 * - 消息列表（用户/AI 气泡）
 * - SSE 流式接收（打字机效果）
 * - 坑点标识（AI 坑点消息显示徽章）
 * - 输入框 + 发送按钮 + Enter 发送
 * - 加载/错误状态
 * - 自动滚动到底部
 */
export function ChatWindow({
  sessionId,
  conversationType = "training_assistant",
  conversationId: initialConversationId,
  initialMessages = [],
  placeholder = "输入你的问题...",
  onConversationCreated,
  onMessageDone,
  onUserQuestion,
  onApprovalResolved,
  height = "500px",
  className = "",
}: ChatWindowProps) {
  const isSessionAssistant=conversationType === "training_assistant" && !!sessionId;
  const sessionConversation=useSessionConversation(sessionId,isSessionAssistant);
  const canReply=!isSessionAssistant || sessionConversation.coordinator.canSend(sessionId);
  const sessionKey=`${conversationType}:${sessionId??"none"}`;
  const currentSessionKey=useRef(sessionKey);currentSessionKey.current=sessionKey;
  const replyEpoch=useRef(0);
  const sendLock=useRef(false);
  const [messages, setMessages] = useState<ChatMessage[]>(initialMessages);
  const [inputValue, setInputValue] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [conversationId, setConversationId] = useState<number | undefined>(initialConversationId);
  const [isCreatingConv, setIsCreatingConv] = useState(false);
  // 待用户确认的修改类步骤（awaiting_approval）。
  const [pendingApproval, setPendingApproval] = useState<StepItem | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(()=>{
    replyEpoch.current++;abortRef.current?.abort();abortRef.current=null;sendLock.current=false;
    setPendingApproval(null);setIsSending(false);setInputValue("");setMessages(isSessionAssistant?[]:initialMessages);setConversationId(isSessionAssistant?undefined:initialConversationId);
  },[sessionId,conversationType]);
  useEffect(()=>{
    const state=sessionConversation.state;
    if(!isSessionAssistant||state.sessionId!==sessionId||state.loading||state.error||!state.conversationId)return;
    setConversationId(state.conversationId);
    setMessages(prev=>mergeSessionMessages(state.messages,prev));
  },[isSessionAssistant,sessionId,sessionConversation.state]);
  // 自动滚动到底部
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  // 确保对话存在
  async function ensureConversation(): Promise<number> {
    if(isSessionAssistant)return sessionConversation.coordinator.requireConversation(sessionId);
    if (conversationId) return conversationId;

    setIsCreatingConv(true);
    try {
      const resp = await createConversation({
        session_id: sessionId,
        conversation_type: conversationType,
      });
      setConversationId(resp.conversation_id);
      onConversationCreated?.(resp.conversation_id);
      return resp.conversation_id;
    } finally {
      setIsCreatingConv(false);
    }
  }

  // 发送消息
  async function handleSend() {
    const content = inputValue.trim();
    if (!content || isSending || sendLock.current || !canReply) return;
    sendLock.current=true;
    const epoch=++replyEpoch.current;
    const key=sessionKey;
    const isCurrent=()=>replyEpoch.current===epoch&&currentSessionKey.current===key;

    onUserQuestion?.(content);

    // 先追加用户消息
    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content,
    };
    setMessages((prev) => [...prev, userMsg]);
    setInputValue("");
    setIsSending(true);

    // 重置 textarea 高度
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }

    try {
      const convId = await ensureConversation();
      if(!isCurrent())return;

      // 添加 AI 占位消息（流式输出）
      const aiMsgId = `ai-${Date.now()}`;
      setMessages((prev) => [
        ...prev,
        {
          id: aiMsgId,
          role: "assistant",
          content: "",
          isStreaming: true,
        },
      ]);

      abortRef.current = sendMessageStream(convId, content, {
        onThinking: () => {
          if(!isCurrent())return;
          // 思考中，保持占位
        },
        onStep: (step) => {
          if(!isCurrent())return;
          // 修改类工具挂起：步骤照常入时间线，同时弹出确认窗。
          if (step.state === "awaiting_approval" && step.approvalId) {
            setPendingApproval(step);
          } else {
            // 同一步骤已有终态/执行中：审批已被消费，关闭弹窗。
            setPendingApproval((cur) => (cur?.stepId === step.stepId ? null : cur));
          }
          setMessages((prev) =>
            prev.map((m) => {
              if (m.id !== aiMsgId) return m;
              const steps = [...(m.steps ?? [])];
              const idx = steps.findIndex((s) => s.stepId === step.stepId);
              if (idx >= 0) {
                steps[idx] = { ...steps[idx], ...step };
              } else {
                steps.push(step);
              }
              return { ...m, steps };
            }),
          );
        },
        onDelta: (delta, event) => {
          if(!isCurrent())return;
          setMessages((prev) =>
            prev.map((m) =>
              m.id === aiMsgId
                ? {
                    ...m,
                    content: m.content + delta,
                    isPitfall: event.is_pitfall ?? m.isPitfall,
                    pitfallType: event.pitfall_type ?? m.pitfallType,
                    confidenceLevel: event.confidence_level ?? m.confidenceLevel,
                  }
                : m,
            ),
          );
        },
        onDone: (event) => {
          if(!isCurrent())return;
          setMessages((prev) =>
            prev.map((m) =>
              m.id === aiMsgId
                ? {
                    ...m,
                    isStreaming: false,
                    id: event.message_id ?? m.id,
                    isPitfall: event.is_pitfall ?? m.isPitfall,
                    pitfallType: event.pitfall_type ?? m.pitfallType,
                    confidenceLevel: event.confidence_level ?? m.confidenceLevel,
                  }
                : m,
            ),
          );
          setIsSending(false);
          sendLock.current=false;
          abortRef.current = null;
          onMessageDone?.(event);
        },
        onError: (errorMsg) => {
          if(!isCurrent())return;
          setMessages((prev) =>
            prev.map((m) =>
              m.id === aiMsgId
                ? {
                    ...m,
                    isStreaming: false,
                    isError: true,
                    errorMsg,
                    content: "",
                  }
                : m,
            ),
          );
          setIsSending(false);
          sendLock.current=false;
          abortRef.current = null;
        },
      });
    } catch (err: unknown) {
      if(!isCurrent())return;
      sendLock.current=false;
      // 创建对话失败
      setMessages((prev) => [
        ...prev,
        {
          id: `err-${Date.now()}`,
          role: "assistant",
          content: "",
          isError: true,
          errorMsg: (err as Error)?.message ?? "发送失败",
        },
      ]);
      setIsSending(false);
    }
  }

  // 回车发送，Shift+Enter 换行
  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  // 输入框自适应高度
  function handleInput(e: React.ChangeEvent<HTMLTextAreaElement>) {
    setInputValue(e.target.value);
    const ta = e.target;
    ta.style.height = "auto";
    ta.style.height = `${Math.min(ta.scrollHeight, 120)}px`;
  }

  // 组件卸载时取消请求
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  // 投递审批决定；失败时把后端错误信息返回给弹窗展示。
  async function handleResolveApproval(approvalId: string, approved: boolean): Promise<string | null> {
    if (!conversationId) return "对话尚未创建";
    const approvalEpoch=replyEpoch.current;const approvalSession=sessionKey;
    try {
      await resolveApproval(conversationId, approvalId, approved);
      if(approvalEpoch!==replyEpoch.current||currentSessionKey.current!==approvalSession)return "班次已切换，原审批结果不会应用到当前班次";
      onApprovalResolved?.(approved);
      return null;
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { msg?: string } } })?.response?.data?.msg;
      return msg || (err as Error)?.message || "提交失败，请重试";
    }
  }

  return (
    <div
      className={`flex flex-col bg-ink-50 border border-ink-100 rounded-2xl overflow-hidden ${className}`}
      style={{ height }}
    >
      <ApprovalModal
        step={pendingApproval}
        onResolve={handleResolveApproval}
        onClose={() => setPendingApproval(null)}
      />
      {isSessionAssistant && (
        <div className="px-4 py-2 text-xs text-ink-500 border-b border-ink-100" role="status">
          {sessionConversation.state.loading || sessionConversation.state.sessionId!==sessionId ? "正在恢复本班次对话…" : sessionConversation.state.error ? <><span>对话恢复失败：{sessionConversation.state.error}</span><button className="ml-2 text-brand-600" onClick={()=>void sessionConversation.retry()}>重试</button></> : sessionConversation.state.status!=="running" ? (["completed","failed","aborted","ended"].includes(sessionConversation.state.status)?"班次已结束，历史记录只读":sessionConversation.state.status==="unavailable"?"班次状态暂不可确认，正在重新连接…":"班次正在准备，稍后即可提问") : "本班次对话已接续"}
        </div>
      )}
      {/* 消息列表区 */}
      <div className="flex-1 overflow-y-auto p-5 space-y-5">
        {isSessionAssistant && sessionConversation.state.hasMore && <button className="block mx-auto text-xs text-brand-600" disabled={sessionConversation.state.loadingHistory} onClick={()=>void sessionConversation.loadOlder()}>{sessionConversation.state.loadingHistory?"正在读取…":"查看更早的对话"}</button>}
        {isSessionAssistant && sessionConversation.state.historyError && <p className="text-xs text-red-600" role="alert">{sessionConversation.state.historyError}</p>}
        {messages.length === 0 && !isCreatingConv && canReply && (
          <div className="h-full flex flex-col items-center justify-center text-center text-ink-400 py-10">
            <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-ai/20 to-brand-100 flex items-center justify-center mb-4">
              <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-ai">
                <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                <line x1="12" x2="12" y1="19" y2="22" />
              </svg>
            </div>
            <p className="font-medium text-ink-600 mb-1">AI 助教已就绪</p>
            <p className="text-[13px] text-ink-400 max-w-xs">
              有任何排障或技术问题都可以问我。
              <br />
              注意：我的建议可能包含错误，请独立验证判断。
            </p>
          </div>
        )}

        {messages.map((msg) => (
          <MessageBubble key={msg.id} message={msg} />
        ))}

        {isCreatingConv && (
          <div className="flex gap-3">
            <div className="flex-none w-9 h-9 rounded-xl bg-gradient-to-br from-ai to-ai/80 text-white flex items-center justify-center shadow-sm">
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                <line x1="12" x2="12" y1="19" y2="22" />
              </svg>
            </div>
            <div className="bg-white border border-ink-100 rounded-2xl rounded-tl-md px-4 py-3">
              <div className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-full bg-ink-300 animate-bounce" style={{ animationDelay: "0ms" }} />
                <span className="w-2 h-2 rounded-full bg-ink-300 animate-bounce" style={{ animationDelay: "150ms" }} />
                <span className="w-2 h-2 rounded-full bg-ink-300 animate-bounce" style={{ animationDelay: "300ms" }} />
              </div>
            </div>
          </div>
        )}

        {isSessionAssistant && sessionId && <OperationTimeline key={sessionId} sessionId={sessionId} actor="assistant" canApprove={canReply}/> }
        <div ref={messagesEndRef} />
      </div>

      {/* 输入区 */}
      <div className="border-t border-ink-100 bg-white p-3">
        <div className="flex items-end gap-2 bg-ink-50 rounded-xl border border-ink-200 focus-within:border-brand-300 focus-within:ring-4 focus-within:ring-brand-500/10 transition-all">
          <textarea
            ref={textareaRef}
            value={inputValue}
            onChange={handleInput}
            onKeyDown={handleKeyDown}
            placeholder={isSending ? "AI 正在思考..." : placeholder}
            disabled={isSending || isCreatingConv || !canReply}
            rows={1}
            className="flex-1 bg-transparent px-4 py-3 text-[14px] text-ink-700 placeholder:text-ink-400 resize-none focus:outline-none disabled:opacity-50 disabled:cursor-not-allowed"
          />
          <button
            type="button"
            onClick={handleSend}
            disabled={!inputValue.trim() || isSending || isCreatingConv || !canReply}
            className="m-2 w-9 h-9 rounded-lg bg-brand-500 hover:bg-brand-600 disabled:bg-ink-200 disabled:cursor-not-allowed text-white flex items-center justify-center transition-colors flex-none"
            aria-label="发送"
          >
            {isSending ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="animate-spin">
                <path d="M21 12a9 9 0 1 1-6.219-8.56" />
              </svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="m22 2-7 20-4-9-9-4Z" />
                <path d="M22 2 11 13" />
              </svg>
            )}
          </button>
        </div>
        <p className="mt-2 text-[11px] text-ink-400 text-center">
          按 Enter 发送，Shift + Enter 换行 · AI 建议仅供参考，请独立验证
        </p>
      </div>
    </div>
  );
}
