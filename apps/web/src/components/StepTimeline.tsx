import { useState } from "react";
import type { StepItem } from "../services/chat";

interface StepTimelineProps {
  steps: StepItem[];
}

/** 各状态对应的图标与颜色。 */
function StepIcon({ state }: { state: string }) {
  if (state === "completed") {
    return (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" className="text-ok flex-none">
        <polyline points="20 6 9 17 4 12" />
      </svg>
    );
  }
  if (state === "failed") {
    return (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="2.5" strokeLinecap="round" className="text-danger flex-none">
        <line x1="18" y1="6" x2="6" y2="18" />
        <line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    );
  }
  if (state === "awaiting_approval") {
    return (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-warn flex-none">
        <circle cx="12" cy="12" r="10" />
        <line x1="12" y1="8" x2="12" y2="12" />
        <line x1="12" y1="16" x2="12.01" y2="16" />
      </svg>
    );
  }
  if (state === "rejected") {
    return (
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="text-ink-400 flex-none">
        <circle cx="12" cy="12" r="10" />
        <line x1="15" y1="9" x2="9" y2="15" />
        <line x1="9" y1="9" x2="15" y2="15" />
      </svg>
    );
  }
  // started / progress：旋转指示。
  return (
    <span
      className="inline-block w-[14px] h-[14px] border-2 border-brand flex-none rounded-full animate-spin"
      style={{ borderTopColor: "transparent" }}
    />
  );
}

/** 单个步骤（可展开查看详情）。 */
function StepRow({ step }: { step: StepItem }) {
  const [open, setOpen] = useState(false);
  const running = step.state === "started" || step.state === "progress";
  const hasDetail = !!step.detailJson && step.detailJson !== "{}";
  const label = step.display || step.tool || "执行中";

  return (
    <div className="text-[12.5px] leading-relaxed">
      <button
        type="button"
        onClick={() => hasDetail && setOpen((o) => !o)}
        className={`w-full flex items-center gap-2 text-left px-2 py-1.5 rounded-lg transition-colors ${
          hasDetail ? "hover:bg-ink-50" : "cursor-default"
        } ${running ? "text-brand font-medium" : "text-ink-500"}`}
      >
        <StepIcon state={step.state} />
        <span className="truncate">{label}</span>
        {step.summary && (
          <span className="ml-auto text-[11px] text-ink-400 flex-none">{step.summary}</span>
        )}
        {hasDetail && (
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
            className={`flex-none text-ink-300 transition-transform ${open ? "rotate-90" : ""}`}>
            <polyline points="9 18 15 12 9 6" />
          </svg>
        )}
      </button>
      {open && hasDetail && (
        <pre className="mx-2 mb-1 mt-0.5 bg-ink-50 border border-ink-100 rounded-lg p-2 text-[11px] leading-relaxed text-ink-600 overflow-x-auto whitespace-pre-wrap break-all">
          {prettyJSON(step.detailJson!)}
        </pre>
      )}
    </div>
  );
}

function prettyJSON(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

/**
 * StepTimeline 助手工具执行步骤时间线（过程透明）。
 * 渲染在 AI 气泡正文上方，对齐 ChatGPT/Trae 的执行过程展示。
 */
export function StepTimeline({ steps }: StepTimelineProps) {
  if (!steps || steps.length === 0) return null;
  return (
    <div className="mb-2 bg-white border border-ink-100 rounded-xl shadow-sm overflow-hidden">
      <div className="px-3 pt-2 pb-1 text-[11px] font-semibold text-ink-400 tracking-wide">
        助手操作过程
      </div>
      <div className="px-1 pb-1">
        {steps.map((s) => (
          <StepRow key={s.stepId} step={s} />
        ))}
      </div>
    </div>
  );
}
