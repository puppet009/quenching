interface SandboxLaunchModalProps {
  open: boolean;
  /** 沙箱启动进度（0-100） */
  progressPercent: number;
  /** 当前阶段文案 */
  progressStage: string;
  /** 启动失败提示；非空时展示重试按钮 */
  errorMessage: string;
  confirming: boolean;
  onClose: () => void;
  onRetry: () => void;
}

/**
 * 沙箱启动进度弹窗（一键进入实训：难度由系统判定，不再让用户选）。
 */
export function SandboxLaunchModal({
  open,
  progressPercent,
  progressStage,
  errorMessage,
  confirming,
  onClose,
  onRetry,
}: SandboxLaunchModalProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-ink-900/60 backdrop-blur-sm animate-fade-in" onClick={confirming ? undefined : onClose} />

      <div className="relative w-full max-w-md bg-white rounded-2xl shadow-2xl overflow-hidden animate-scale-in">
        <div className="px-7 pt-6 pb-5 border-b border-ink-100">
          <h2 className="text-lg font-bold text-ink-900 mb-1">正在准备训练环境</h2>
          <p className="text-[13px] text-ink-500">
            故障内容在环境就绪后才会出现，请留意业务异常信号
          </p>
        </div>

        <div className="px-7 py-6">
          <div className="flex items-center justify-between mb-2 text-[12px]">
            <span className="text-ink-600 flex items-center gap-2">
              {confirming && (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" className="animate-spin text-brand-500" />
              )}
              {progressStage || "初始化中..."}
            </span>
            <span className="font-semibold text-brand-600">{Math.round(progressPercent)}%</span>
          </div>
          <div className="h-2 rounded-full bg-ink-100 overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-brand-400 to-brand-600 rounded-full transition-all duration-500"
              style={{ width: `${Math.min(100, Math.max(0, progressPercent))}%` }}
            />
          </div>

          {errorMessage && (
            <div className="mt-4 rounded-lg bg-danger/10 border border-danger/20 px-3 py-2 text-[12px] text-danger">
              {errorMessage}
            </div>
          )}
        </div>

        <div className="px-7 pb-6 flex gap-3">
          <button
            type="button"
            onClick={onClose}
            disabled={confirming}
            className="flex-1 h-11 rounded-xl border border-ink-200 bg-white hover:bg-ink-50 text-ink-700 font-medium text-[14px] transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            取消
          </button>
          {errorMessage ? (
            <button
              type="button"
              onClick={onRetry}
              className="flex-1 h-11 rounded-xl bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[14px] transition-colors"
            >
              重试
            </button>
          ) : (
            <button
              type="button"
              disabled
              className="flex-1 h-11 rounded-xl bg-ink-200 text-white font-semibold text-[14px] cursor-not-allowed flex items-center justify-center gap-2"
            >
              {confirming && (
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" className="animate-spin" />
              )}
              环境就绪后自动进入
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
