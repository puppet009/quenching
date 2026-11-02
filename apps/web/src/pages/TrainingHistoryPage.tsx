import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { BackgroundTrainingProgress } from "../components/training/BackgroundTrainingProgress";
import { Pagination, formatIsoMinute } from "@quenching/shared";
import {
  listReviewHistory,
  formatDuration,
  GEN_STATUS_LABELS,
  type GenerationStatus,
  type ReviewListItem,
} from "../services/review";

/**
 * 复盘历史列表页（T4-16 真实接口版）
 *
 * 数据：GET /api/v1/reviews/history?page&size 真分页
 * 能力：
 * - 本页标题关键词过滤（后端暂不支持搜索，仅过滤当前页）
 * - 生成状态筛选（全部 / 已完成 / 生成中 / 失败）
 * - 点击行或「查看报告」进入 /review/:id
 * - 未完成的复盘同样可进入详情页继续等待生成
 *
 * 访问路径: /history
 */
const PAGE_SIZE = 10;

type StatusFilter = "" | GenerationStatus;

export function TrainingHistoryPage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const requestedSessionId = Number(searchParams.get("session"));
  const sessionId = Number.isSafeInteger(requestedSessionId) && requestedSessionId > 0 ? requestedSessionId : undefined;
  const requestRef = useRef(0);
  const pendingRequestRef = useRef<{ page: number; request: number } | null>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [items, setItems] = useState<ReviewListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [errorMsg, setErrorMsg] = useState("");
  const [keyword, setKeyword] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("");

  const loadPage = useCallback((targetPage: number, quiet = false) => {
    if (quiet && pendingRequestRef.current?.page === targetPage) return;
    const request = ++requestRef.current;
    pendingRequestRef.current = { page: targetPage, request };
    if (!quiet) setLoading(true);
    listReviewHistory({ page: targetPage, size: PAGE_SIZE })
      .then((res) => {
        if (request !== requestRef.current) return;
        setItems(res.list || []);
        setTotal(res.total || 0);
        setErrorMsg("");
      })
      .catch((err: unknown) => {
        if (request !== requestRef.current) return;
        setErrorMsg(err instanceof Error ? err.message : "复盘历史加载失败");
      })
      .finally(() => {
        if (pendingRequestRef.current?.request === request) pendingRequestRef.current = null;
        if (request === requestRef.current) setLoading(false);
      });
  }, []);

  useEffect(() => {
    loadPage(page);
    return () => { requestRef.current += 1; };
  }, [page, loadPage]);

  const refreshReviews = useCallback(() => loadPage(page, true), [page, loadPage]);

  useEffect(() => {
    if (!items.some(item => item.generation_status === "pending" || item.generation_status === "generating")) return;
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") loadPage(page, true);
    }, 5000);
    return () => clearInterval(timer);
  }, [items, page, loadPage]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // 后端暂无搜索/状态过滤参数，在当前页数据上做轻量过滤
  const visibleItems = items.filter((item) => {
    if (statusFilter && item.generation_status !== statusFilter) return false;
    if (keyword.trim()) {
      return (item.title || "").toLowerCase().includes(keyword.trim().toLowerCase());
    }
    return true;
  });

  const goToPage = (p: number) => {
    if (p < 1 || p > totalPages) return;
    setPage(p);
    window.scrollTo({ top: 0, behavior: "smooth" });
  };

  return (
    <div>
      <div className="max-w-5xl mx-auto px-6 py-8">
        {/* 页头（顶部导航由 AppLayout 统一提供） */}
        <div className="flex items-center justify-between gap-4 mb-5">
          <div>
            <h1 className="text-2xl font-bold text-ink-900">我的训练</h1>
            <p className="text-[13px] text-ink-400 mt-1">共 {total} 份复盘报告</p>
          </div>
          <button
            type="button"
            onClick={() => navigate("/projects")}
            className="h-9 px-4 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium flex items-center transition-colors shrink-0"
          >
            去训练
          </button>
        </div>
        <BackgroundTrainingProgress sessionId={sessionId} onRefreshReviews={refreshReviews} />
        {/* 筛选栏 */}
        <div className="bg-white rounded-2xl border border-ink-100 shadow-sm p-4 mb-5">
          <div className="flex flex-col md:flex-row gap-3">
            {/* 搜索框 */}
            <div className="relative flex-1">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-ink-400">🔍</span>
              <input
                type="text"
                placeholder="搜索复盘标题..."
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                className="w-full h-10 pl-10 pr-3 rounded-xl border border-ink-200 text-sm text-ink-700 focus:outline-none focus:border-brand-400 focus:ring-2 focus:ring-brand-100 transition-all"
              />
            </div>

            {/* 状态筛选 */}
            <div className="flex items-center gap-1.5 bg-ink-50 rounded-xl p-1">
              <span className="text-xs text-ink-400 px-2">状态</span>
              {(
                [
                  { value: "", label: "全部" },
                  { value: "completed", label: "已完成" },
                  { value: "generating", label: "生成中" },
                  { value: "failed", label: "失败" },
                ] as { value: StatusFilter; label: string }[]
              ).map((opt) => (
                <button
                  key={opt.value || "all"}
                  onClick={() => setStatusFilter(opt.value)}
                  className={`h-7 px-3 rounded-lg text-xs font-medium transition-colors ${
                    statusFilter === opt.value
                      ? "bg-white text-brand-600 shadow-sm"
                      : "text-ink-500 hover:text-ink-700"
                  }`}
                >
                  {opt.label}
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* 列表 */}
        {loading ? (
          <div className="bg-white rounded-2xl border border-ink-100 shadow-sm p-10 text-center text-ink-400">
            加载中...
          </div>
        ) : errorMsg ? (
          <div className="bg-white rounded-2xl border border-danger-200 shadow-sm p-10 text-center">
            <div className="text-sm text-danger-600 font-medium mb-3">{errorMsg}</div>
            <button
              onClick={() => loadPage(page)}
              className="h-9 px-5 rounded-xl border border-ink-200 text-ink-600 text-[13px] font-medium hover:bg-ink-50 transition-colors"
            >
              重新加载
            </button>
          </div>
        ) : visibleItems.length > 0 ? (
          <div className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden">
            {/* 表头 */}
            <div className="hidden md:grid grid-cols-12 gap-3 px-5 py-3 bg-ink-50 border-b border-ink-100 text-xs font-medium text-ink-500">
              <div className="col-span-5">复盘标题</div>
              <div className="col-span-2">生成时间</div>
              <div className="col-span-1 text-center">技术分</div>
              <div className="col-span-1 text-center">协作分</div>
              <div className="col-span-1 text-center">状态</div>
              <div className="col-span-2 text-right">操作</div>
            </div>

            <div className="divide-y divide-ink-50">
              {visibleItems.map((item) => (
                <HistoryRow
                  key={item.id}
                  item={item}
                  onOpen={() => navigate(`/review/${item.id}`)}
                />
              ))}
            </div>
          </div>
        ) : (
          <EmptyState onGoTraining={() => navigate("/projects")} />
        )}

        {/* 真分页 */}
        {!loading && !errorMsg && total > 0 && (
          <Pagination
            currentPage={page}
            totalPages={totalPages}
            totalItems={total}
            pageSize={PAGE_SIZE}
            onPageChange={goToPage}
          />
        )}
      </div>
    </div>
  );
}

/* ===== 子组件 ===== */

function HistoryRow({
  item,
  onOpen,
}: {
  item: ReviewListItem;
  onOpen: () => void;
}) {
  const status = item.generation_status;
  const isCompleted = status === "completed";

  return (
    <div
      onClick={onOpen}
      className="grid grid-cols-1 md:grid-cols-12 gap-3 px-5 py-4 items-center hover:bg-ink-50/60 transition-colors cursor-pointer"
    >
      {/* 标题 */}
      <div className="md:col-span-5 flex items-center gap-3 min-w-0">
        <div className="w-10 h-10 rounded-xl flex items-center justify-center text-lg flex-shrink-0 bg-brand-50">
          📝
        </div>
        <div className="min-w-0">
          <div className="text-sm font-semibold text-ink-800 truncate">
            {item.title || "未命名复盘"}
          </div>
          <div className="text-xs text-ink-400 mt-0.5">
            会话 #{item.session_id} · 耗时 {formatDuration(item.generation_duration_ms)}
          </div>
        </div>
      </div>

      {/* 时间 */}
      <div className="md:col-span-2 text-xs text-ink-500">
        {formatDateTime(item.created_at)}
      </div>

      {/* 技术分 */}
      <div className="md:col-span-1 text-center">
        <span className={`text-base font-bold ${scoreColor(item.tech_score)}`}>
          {isCompleted && item.tech_score > 0 ? item.tech_score : "—"}
        </span>
      </div>

      {/* 协作分 */}
      <div className="md:col-span-1 text-center">
        <span className={`text-base font-bold ${scoreColor(item.collab_score)}`}>
          {isCompleted && item.collab_score > 0 ? item.collab_score : "—"}
        </span>
      </div>

      {/* 状态 */}
      <div className="md:col-span-1 text-center">
        <StatusPill status={status} />
      </div>

      {/* 操作 */}
      <div className="md:col-span-2 flex md:justify-end">
        <button
          onClick={(e) => {
            e.stopPropagation();
            onOpen();
          }}
          className={`h-8 px-3 rounded-lg text-xs font-medium transition-colors ${
            isCompleted
              ? "bg-brand-50 text-brand-600 hover:bg-brand-100"
              : status === "failed"
                ? "border border-danger-200 text-danger-600 hover:bg-danger-50"
                : "border border-ink-200 text-ink-600 hover:bg-ink-50"
          }`}
        >
          {isCompleted ? "查看报告" : status === "failed" ? "查看详情" : "生成中"}
        </button>
      </div>
    </div>
  );
}

function StatusPill({ status }: { status: GenerationStatus }) {
  const config: Record<GenerationStatus, { cls: string; pulse?: boolean }> = {
    completed: { cls: "bg-ok-50 text-ok-600" },
    generating: { cls: "bg-warn-50 text-warn-600", pulse: true },
    pending: { cls: "bg-ink-100 text-ink-500", pulse: true },
    failed: { cls: "bg-danger-50 text-danger-600" },
  };
  const c = config[status];
  return (
    <span
      className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium ${c.cls}`}
    >
      <span className={`w-1.5 h-1.5 rounded-full bg-current ${c.pulse ? "animate-pulse" : ""}`} />
      {GEN_STATUS_LABELS[status] || status}
    </span>
  );
}

function scoreColor(score: number): string {
  if (score <= 0) return "text-ink-300";
  if (score >= 90) return "text-ok-600";
  if (score >= 80) return "text-brand-600";
  if (score >= 70) return "text-warn-600";
  return "text-danger-600";
}

function formatDateTime(s: string): string {
  return formatIsoMinute(s);
}

function EmptyState({ onGoTraining }: { onGoTraining: () => void }) {
  return (
    <div className="bg-white rounded-2xl border border-ink-100 shadow-sm py-16 text-center">
      <div className="text-5xl mb-4">📭</div>
      <div className="text-base font-semibold text-ink-700 mb-1">暂无复盘记录</div>
      <div className="text-sm text-ink-400 mb-6">训练结束后，系统会在后台生成复盘报告。报告就绪后可在这里查看。</div>
      <button
        onClick={onGoTraining}
        className="h-10 px-6 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors"
      >
        去训练
      </button>
    </div>
  );
}
