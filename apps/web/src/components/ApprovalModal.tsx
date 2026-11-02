import { useEffect, useState } from "react";
import type { StepItem } from "../services/chat";

export interface ApprovalModalProps {
  /** 待审批步骤（null 时不显示） */
  step: StepItem | null;
  /** 提交审批决定；返回错误信息字符串表示失败，返回 null 表示成功 */
  onResolve: (approvalId: string, approved: boolean) => Promise<string | null>;
  /** 弹窗关闭（仅在已提交成功后触发） */
  onClose: () => void;
}

/** 参数中文标签。 */
const FIELD_LABELS: Record<string, string> = {
  pod: "目标服务器",
  container: "容器",
  path: "文件路径",
  content: "文件内容",
};

/** 风险提示：按工具类型给出。 */
function riskNote(tool?: string): string {
  if (tool === "write_file") {
    return "该操作会在服务器上写入文件；若目标文件已存在，原内容将被覆盖且不会自动备份。";
  }
  return "该操作会改变实训环境的实际状态，请确认参数无误。";
}

/**
 * ApprovalModal 修改类工具的用户确认弹窗（M4）。
 * 展示动作、参数与风险，用户同意后才真正执行；拒绝则助手改为手动引导。
 */
export function ApprovalModal({ step, onResolve, onClose }: ApprovalModalProps) {
  const [submitting, setSubmitting] = useState<"approve" | "reject" | null>(null);
  const [errorMsg, setErrorMsg] = useState("");

  // 每次弹出新审批时重置状态。
  useEffect(() => {
    setSubmitting(null);
    setErrorMsg("");
  }, [step?.approvalId]);

  if (!step) return null;

  let args: Record<string, unknown>;
  try {
    args = step.detailJson ? JSON.parse(step.detailJson) : {};
  } catch {
    args = {};
  }
  const fields = Object.entries(args).filter(([, v]) => v !== undefined && v !== "");
  const contentValue = typeof args.content === "string" ? args.content : "";

  async function handleResolve(approved: boolean) {
    if (!step?.approvalId || submitting) return;
    setSubmitting(approved ? "approve" : "reject");
    setErrorMsg("");
    const err = await onResolve(step.approvalId, approved);
    if (err) {
      setErrorMsg(err);
      setSubmitting(null);
      return;
    }
    setSubmitting(null);
    onClose();
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink-900/40 backdrop-blur-sm p-4"
      role="dialog"
      aria-modal="true"
      aria-label="操作确认"
    >
      <div className="w-full max-w-lg bg-white rounded-2xl shadow-2xl overflow-hidden">
        {/* 头部 */}
        <div className="flex items-start gap-3 px-5 pt-5 pb-4 border-b border-ink-100">
          <div className="w-10 h-10 rounded-xl bg-warn/10 text-warn flex items-center justify-center flex-none">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
              <line x1="12" y1="9" x2="12" y2="13" />
              <line x1="12" y1="17" x2="12.01" y2="17" />
            </svg>
          </div>
          <div className="min-w-0">
            <h3 className="text-[15px] font-semibold text-ink-800">助手请求执行修改操作</h3>
            <p className="text-[12.5px] text-ink-500 mt-0.5">
              {step.display || step.tool} · 需经你确认后才会执行
            </p>
          </div>
        </div>

        {/* 参数区 */}
        <div className="px-5 py-4 space-y-3 max-h-[45vh] overflow-y-auto">
          {fields.length === 0 && (
            <p className="text-[12.5px] text-ink-400">该操作未提供参数明细。</p>
          )}
          {fields.map(([key, value]) => {
            const isContent = key === "content";
            return (
              <div key={key}>
                <div className="text-[11.5px] font-medium text-ink-400 mb-1">
                  {FIELD_LABELS[key] ?? key}
                </div>
                {isContent ? (
                  <pre className="bg-ink-50 border border-ink-100 rounded-lg p-2.5 text-[12px] leading-relaxed text-ink-700 max-h-40 overflow-auto whitespace-pre-wrap break-all">
                    {contentValue || "(空)"}
                  </pre>
                ) : (
                  <div className="bg-ink-50 border border-ink-100 rounded-lg px-2.5 py-2 text-[12.5px] text-ink-700 break-all">
                    {String(value)}
                  </div>
                )}
              </div>
            );
          })}

          {/* 风险提示 */}
          <div className="flex gap-2 bg-warn/5 border border-warn/20 rounded-lg px-3 py-2">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
              strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
              className="text-warn flex-none mt-0.5">
              <circle cx="12" cy="12" r="10" />
              <line x1="12" y1="8" x2="12" y2="12" />
              <line x1="12" y1="16" x2="12.01" y2="16" />
            </svg>
            <p className="text-[12px] text-ink-600 leading-relaxed">{riskNote(step.tool)}</p>
          </div>

          {errorMsg && (
            <p className="text-[12px] text-danger">{errorMsg}</p>
          )}
        </div>

        {/* 按钮区 */}
        <div className="flex items-center justify-end gap-2 px-5 py-4 bg-ink-50 border-t border-ink-100">
          <button
            type="button"
            onClick={() => handleResolve(false)}
            disabled={submitting !== null}
            className="px-4 py-2 rounded-lg text-[13px] font-medium bg-white border border-ink-200 text-ink-600 hover:bg-ink-100 disabled:opacity-50 transition-colors"
          >
            {submitting === "reject" ? "提交中..." : "拒绝执行"}
          </button>
          <button
            type="button"
            onClick={() => handleResolve(true)}
            disabled={submitting !== null}
            className="px-4 py-2 rounded-lg text-[13px] font-medium bg-brand-500 text-white hover:bg-brand-600 disabled:opacity-50 transition-colors"
          >
            {submitting === "approve" ? "执行中..." : "同意并执行"}
          </button>
        </div>
      </div>
    </div>
  );
}
