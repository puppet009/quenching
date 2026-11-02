import {
  type ChatMessage,
  CONFIDENCE_LABELS,
  PITFALL_TYPE_LABELS,
} from "../services/chat";
import { StepTimeline } from "./StepTimeline";

interface MessageBubbleProps {
  message: ChatMessage;
}

/**
 * 消息气泡组件
 * - 用户消息：右侧，品牌色背景，白色文字
 * - AI 消息：左侧，白色背景，深色文字
 * - 坑点标识：AI 坑点消息显示特殊徽章
 * - 流式输出：正在生成时显示光标动画
 */
export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";

  if (isUser) {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%]">
          <div className="bg-brand-500 text-white rounded-2xl rounded-tr-md px-4 py-3 text-[14px] leading-relaxed shadow-sm">
            {message.content}
          </div>
        </div>
      </div>
    );
  }

  // AI 消息
  return (
    <div className="flex gap-3">
      {/* AI 头像 */}
      <div className="flex-none w-9 h-9 rounded-xl bg-gradient-to-br from-ai to-ai/80 text-white flex items-center justify-center shadow-sm">
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
          <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
          <line x1="12" x2="12" y1="19" y2="22" />
        </svg>
      </div>

      <div className="max-w-[80%] flex-1">
        {/* 工具执行步骤时间线（过程透明） */}
        <StepTimeline steps={message.steps ?? []} />
        {/* 坑点徽章 */}
        {message.isPitfall && (
          <div className="mb-2 flex items-center gap-2">
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-danger/10 text-danger text-[11px] font-semibold border border-danger/20">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                <line x1="12" y1="9" x2="12" y2="13" />
                <line x1="12" y1="17" x2="12.01" y2="17" />
              </svg>
              AI 坑点
            </span>
            {message.pitfallType && (
              <span className="text-[11px] text-ink-400">
                {PITFALL_TYPE_LABELS[message.pitfallType] ?? message.pitfallType}
              </span>
            )}
            {message.confidenceLevel && (
              <span className="text-[11px] text-ink-400 ml-auto">
                {CONFIDENCE_LABELS[message.confidenceLevel]}
              </span>
            )}
          </div>
        )}

        {/* 消息正文 */}
        <div
          className={`bg-white border border-ink-100 rounded-2xl rounded-tl-md px-4 py-3 text-[14px] leading-relaxed text-ink-700 shadow-sm ${
            message.isError ? "border-danger/30 bg-danger/5 text-danger" : ""
          }`}
        >
          {message.isError ? (
            <div className="flex items-start gap-2">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="mt-0.5 flex-none">
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="8" x2="12" y2="12" />
                <line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
              <span>{message.errorMsg ?? message.content}</span>
            </div>
          ) : (
            <>
              {message.content}
              {message.isStreaming && (
                <span className="inline-block w-2 h-4 ml-0.5 bg-ink-400 animate-pulse align-text-bottom rounded-sm" />
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
