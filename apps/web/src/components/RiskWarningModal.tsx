import { useEffect, useState } from "react";

interface RiskWarningModalProps {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  /** 项目名称（可选，用于文案中展示） */
  projectName?: string;
  /** 确认中状态（接口请求时禁用按钮） */
  confirming?: boolean;
  /** 接口失败的错误提示（可选，展示在底部） */
  errorMessage?: string;
}

/**
 * 训练前风险告知弹窗（T3-14）
 *
 * 规则：
 * - AI 辅助训练前必弹，纯人工模式不弹
 * - 标准文案：AI 可能遗漏/误判/给不安全建议，请独立验证；沙箱隔离
 * - 不告知坑点数量
 * - 必须勾选"我已阅读并同意"才能点击确认
 * - 确认后调 risk_notice_ack 入库（由调用方处理）
 */
export function RiskWarningModal({
  open,
  onClose,
  onConfirm,
  projectName,
  confirming = false,
  errorMessage = "",
}: RiskWarningModalProps) {
  const [agreed, setAgreed] = useState(false);

  // 每次打开时重置勾选状态
  useEffect(() => {
    if (open) {
      setAgreed(false);
    }
  }, [open]);

  // ESC 关闭
  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !confirming) onClose();
    };
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, [open, onClose, confirming]);

  if (!open) return null;

  const handleConfirm = () => {
    if (!agreed || confirming) return;
    onConfirm();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* 遮罩 */}
      <div
        className="absolute inset-0 bg-ink-900/60 backdrop-blur-sm animate-fade-in"
        onClick={confirming ? undefined : onClose}
      />

      {/* 弹窗主体 */}
      <div className="relative w-full max-w-md bg-white rounded-2xl shadow-2xl overflow-hidden animate-scale-in">
        {/* 顶部警示条 */}
        <div className="bg-gradient-to-r from-warn to-danger px-6 py-4">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-full bg-white/20 flex items-center justify-center">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
                <line x1="12" y1="9" x2="12" y2="13" />
                <line x1="12" y1="17" x2="12.01" y2="17" />
              </svg>
            </div>
            <div>
              <h2 className="text-white font-bold text-[17px]">训练前风险告知</h2>
              <p className="text-white/80 text-[13px]">请仔细阅读以下内容后再开始训练</p>
            </div>
          </div>
        </div>

        {/* 内容区 */}
        <div className="p-6 space-y-5">
          {projectName && (
            <div className="text-[14px] text-ink-600">
              项目：<span className="font-semibold text-ink-800">{projectName}</span>
            </div>
          )}

          <div className="space-y-4">
            <WarningItem
              icon="⚠️"
              title="AI 建议仅供参考"
              desc="AI 助教可能会给出存在遗漏、因果误判或不安全的建议，不能直接作为决策依据。"
            />
            <WarningItem
              icon="🔍"
              title="请保持独立验证"
              desc="所有结论都需要你亲自验证，结合监控、日志、代码等多维度信息做出判断。"
            />
            <WarningItem
              icon="🛡️"
              title="沙箱环境隔离"
              desc="训练在独立的沙箱环境中进行，所有操作不会影响真实生产系统。"
            />
            <WarningItem
              icon="💡"
              title="这是实战训练"
              desc="训练的目标不是找到'正确答案'，而是锻炼你的排障思维和 AI 协作能力。"
            />
          </div>

          {/* 勾选确认 */}
          <label className="flex items-start gap-3 cursor-pointer select-none pt-2">
            <input
              type="checkbox"
              checked={agreed}
              onChange={(e) => setAgreed(e.target.checked)}
              disabled={confirming}
              className="mt-0.5 w-5 h-5 rounded border-ink-300 text-brand-500 focus:ring-brand-500 focus:ring-offset-0 cursor-pointer disabled:opacity-50"
            />
            <span className={`text-[13px] leading-relaxed ${agreed ? "text-ink-700" : "text-ink-500"}`}>
              我已仔细阅读并理解以上风险告知，确认以独立判断参与 AI 辅助训练
            </span>
          </label>
        </div>

        {/* 错误提示 */}
        {errorMessage && (
          <div className="mx-6 mt-2 rounded-lg bg-danger/10 border border-danger/20 px-3 py-2 text-[12px] text-danger">
            {errorMessage}
          </div>
        )}

        {/* 底部按钮 */}
        <div className="px-6 pb-6 flex gap-3 mt-4">
          <button
            type="button"
            onClick={onClose}
            disabled={confirming}
            className="flex-1 h-11 rounded-xl border border-ink-200 bg-white hover:bg-ink-50 text-ink-700 font-medium text-[14px] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleConfirm}
            disabled={!agreed || confirming}
            className="flex-1 h-11 rounded-xl bg-brand-500 hover:bg-brand-600 disabled:bg-ink-200 disabled:cursor-not-allowed text-white font-semibold text-[14px] transition-colors flex items-center justify-center gap-2"
          >
            {confirming && (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" className="animate-spin" />
            )}
            {confirming ? "确认中..." : "确认，开始训练"}
          </button>
        </div>
      </div>
    </div>
  );
}

/** 警示条目 */
function WarningItem({ icon, title, desc }: { icon: string; title: string; desc: string }) {
  return (
    <div className="flex gap-3">
      <div className="w-8 h-8 rounded-lg bg-warn/10 flex items-center justify-center text-base flex-none">
        {icon}
      </div>
      <div className="flex-1 min-w-0">
        <div className="font-semibold text-ink-800 text-[14px] mb-0.5">{title}</div>
        <div className="text-[13px] text-ink-500 leading-relaxed">{desc}</div>
      </div>
    </div>
  );
}
