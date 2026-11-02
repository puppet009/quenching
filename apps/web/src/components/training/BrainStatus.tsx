import { useEffect, useState } from "react";
import { fetchBrainStatus, type BrainSessionStatus } from "../../services/session";

const phases: Record<string, string> = {
  sandbox_ready: "环境准备", baseline_check: "交易基线验证", calibration: "能力校准",
  adaptive_training: "自适应训练", closing: "正在关班", reviewing: "复盘生成中",
  completed: "复盘已完成", degraded: "训练调度受限", failed: "复盘生成失败",
};
const cleanupLabels: Record<string, string> = { pending: "清理中", succeeded: "已清理", failed: "清理待重试" };

export function BrainStatus({ sessionId }: { sessionId: number }) {
  const [state, setState] = useState<BrainSessionStatus | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    if (!Number.isSafeInteger(sessionId) || sessionId <= 0) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    setState(null);
    setUnavailable(false);
    async function poll() {
      let again = true;
      try {
        const value = await fetchBrainStatus(sessionId, controller.signal);
        if (controller.signal.aborted) return;
        setState(value);
        setUnavailable(false);
        again = !(value.cleanup_status === "succeeded" && ["completed", "failed"].includes(value.phase));
      } catch (error) {
        if (controller.signal.aborted) return;
        const status = (error as { response?: { status?: number } }).response?.status;
        if (status === 404) { setState(null); again = false; }
        else setUnavailable(true);
      } finally {
        if (!controller.signal.aborted && again) timer = setTimeout(poll, 5000);
      }
    }
    void poll();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [sessionId]);
  if (!state && !unavailable) return null;
  const closing = state && ["closing", "reviewing", "completed", "failed"].includes(state.phase);
  return <div role="status" className="rounded-lg border border-ink-200 bg-white px-3 py-2 text-xs text-ink-600 max-w-sm shadow-sm">
    {unavailable ? <span>训练状态暂时不可用，正在重试</span> : state && <>
      <span className="font-medium">{phases[state.phase] ?? "状态更新中"}</span>
      {state.current_challenge > 0 && <span> · 第 {state.current_challenge} 个挑战</span>}
      <span> · {state.ability_observed ? "已有能力观察" : "能力证据收集中"}</span>
      {closing && <span> · 沙箱{cleanupLabels[state.cleanup_status] ?? "待确认"}</span>}
      {state.reason && <p className="mt-1">{state.reason}</p>}
    </>}
  </div>;
}
