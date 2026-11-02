import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { Pagination } from "@quenching/shared";
import { fetchSessions } from "../../services/profile";
import { fetchBrainStatus, fetchSessionDetail } from "../../services/session";
import { getReviewStatus } from "../../services/review";
import { trainingReviewLabel, trainingReviewState, type TrainingReviewSession } from "../../services/trainingHistory";

interface BackgroundSession extends TrainingReviewSession {
  id: number;
  projectName: string;
}
const PAGE_SIZE = 20;

function isNotFound(error: unknown): boolean {
  const failure = error as { response?: { status?: number }; code?: number };
  return failure.response?.status === 404 || failure.code === 10004;
}

/** Read-only progress: workers continue even when this component is unmounted. */
export function BackgroundTrainingProgress({ sessionId, onRefreshReviews }: {
  sessionId?: number;
  onRefreshReviews(): void;
}) {
  const [sessions, setSessions] = useState<BackgroundSession[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [errors, setErrors] = useState<string[]>([]);
  const refreshRef = useRef<() => void>(() => {});
  const refreshReviewsRef = useRef(onRefreshReviews);
  refreshReviewsRef.current = onRefreshReviews;

  useEffect(() => {
    let active = true;
    let busy = false;
    let shouldRefresh = true;
    setSessions([]);
    setErrors([]);

    const load = async (refreshReviews = false) => {
      if (busy) return;
      busy = true;
      setLoading(true);
      const failures: string[] = [];
      let recent: BackgroundSession[] | undefined;
      let focused: BackgroundSession | undefined;
      try {
        const response = await fetchSessions({ page, pageSize: PAGE_SIZE, status: "finished" });
        if (!active) return;
        recent = response.list || [];
        setTotal(response.total);
      } catch {
        failures.push("已结束实训的后台状态加载失败，请刷新重试。");
      }

      if (sessionId && !recent?.some(session => session.id === sessionId)) {
        try {
          // The owner-scoped detail request must succeed before showing a focused record.
          const detail = await fetchSessionDetail(sessionId);
          const [brain, review] = await Promise.allSettled([
            fetchBrainStatus(sessionId), getReviewStatus(sessionId),
          ]);
          focused = {
            id: detail.id,
            projectName: detail.projectName,
            status: detail.status,
            hasReview: review.status === "fulfilled",
            reviewId: review.status === "fulfilled" ? review.value.id : undefined,
            brainPhase: brain.status === "fulfilled" ? brain.value.phase : undefined,
            cleanupStatus: brain.status === "fulfilled" ? brain.value.cleanup_status : undefined,
            reviewLookupFailed: review.status === "rejected" && !isNotFound(review.reason),
          };
          if (brain.status === "rejected" && !isNotFound(brain.reason)) {
            failures.push("本次实训的环境回收状态暂不可用，请刷新重试。");
          }
        } catch {
          failures.push("无法读取本次实训记录，请刷新重试。");
        }
      }

      if (!active) return;
      if (recent) {
        const visible = [...recent];
        if (focused) visible.unshift(focused);
        setSessions(visible.filter(session => trainingReviewState(session) !== "active"));
        shouldRefresh = visible.some(session => ["pending", "unavailable"].includes(trainingReviewState(session)));
      } else if (focused) {
        const record = focused;
        setSessions(previous => [record, ...previous.filter(session => session.id !== record.id)].filter(session => trainingReviewState(session) !== "active"));
      }
      if (failures.length) shouldRefresh = true;
      setErrors(failures);
      setLoading(false);
      busy = false;
      if (refreshReviews) refreshReviewsRef.current();
    };

    refreshRef.current = () => { void load(true); };
    void load();
    const timer = setInterval(() => {
      if (shouldRefresh && document.visibilityState === "visible") void load(true);
    }, 5000);
    const onVisible = () => {
      if (shouldRefresh && document.visibilityState === "visible") void load(true);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [sessionId, page]);

  const focused = sessions.find(session => session.id === sessionId);
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  if (!loading && sessions.length === 0 && errors.length === 0 && total === 0) return null;

  return <section className="mb-5 space-y-3" aria-label="实训后台处理进度">
    {focused && ["completed", "aborted", "failed"].includes(focused.status || "") && <div role="status" className="rounded-2xl border border-ok/20 bg-ok/5 px-5 py-4 text-sm text-ok">
      {trainingReviewState(focused) === "available" ? "本次实训已结束，可查看复盘进度。" : "本次实训已结束。环境回收和复盘在后台继续，你可以离开此页，稍后回来查看。"}
    </div>}
    <div className="rounded-2xl border border-ink-100 bg-white p-5 shadow-sm">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-sm font-semibold text-ink-800">已结束的实训</h2>
          <p className="mt-1 text-xs text-ink-500">所有已结束的实训会保留在这里，复盘记录创建后可查看生成进度或报告。</p>
        </div>
        <button type="button" disabled={loading} onClick={() => refreshRef.current()} className="shrink-0 text-xs font-medium text-brand-600 hover:text-brand-700 disabled:text-ink-400">
          {loading ? "刷新中…" : "刷新状态"}
        </button>
      </div>
      {errors.map(message => <p key={message} role="alert" className="mt-3 text-sm text-danger">{message}</p>)}
      {sessions.length > 0 ? <div className="mt-3 divide-y divide-ink-100">
        {sessions.map(session => <div key={session.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
          <div>
            <div className="text-sm font-medium text-ink-800">{session.projectName || "实训项目"} <span className="font-normal text-ink-400">· 会话 #{session.id}</span></div>
            <div className="mt-1 text-xs text-ink-500">{trainingReviewLabel(session)}</div>
          </div>
          {trainingReviewState(session) === "available" ? <Link to={`/review/${session.reviewId}`} className="rounded-lg bg-brand-50 px-3 py-2 text-xs font-medium text-brand-600 hover:bg-brand-100">查看复盘</Link> : <span className="rounded-full bg-warn/10 px-3 py-1 text-xs text-warn">{trainingReviewState(session) === "unavailable" ? "状态待刷新" : "后台处理中"}</span>}
        </div>)}
      </div> : loading ? <p className="mt-3 text-sm text-ink-400">正在读取已结束的实训…</p> : null}
      {total > 0 && <nav aria-label="实训记录分页">
        <Pagination currentPage={page} totalPages={totalPages} totalItems={total} pageSize={PAGE_SIZE} onPageChange={next => {
          if (next >= 1 && next <= totalPages && next !== page) setPage(next);
        }} />
      </nav>}
    </div>
  </section>;
}
