import { GrowthReviewPanel } from "../components/review/GrowthReviewPanel";
import { formatIsoMinute } from "@quenching/shared";
import { getGrowthProgress, type GrowthProgress } from "../services/growthReview";
import { BrainStatus } from "../components/training/BrainStatus";
// 复盘报告页（T4-16 真实接口版）
//
// 数据链路（全部走 services/review.ts → http.ts，自动注入 JWT、统一解包）：
//   1. GET /reviews/:id 拉详情（标量字段平铺 + sections 章节，content 为 Markdown）
//   2. generation_status 非终态时，用详情返回的 session_id 每 3s 轮询
//      GET /reviews/session/:session_id，completed 后重新拉详情；
//      轮询上限 70 次（约 3.5 分钟），组件卸载清理定时器。
//
// 长内容性能：
//   - Markdown 解析为块数组，首屏仅挂载 20 块，IntersectionObserver 哨兵增量追加
//   - 每块 content-visibility: auto + contain-intrinsic-size，跳过离屏渲染
//   - 滚动监听 passive + requestAnimationFrame 节流
//   - 章节目录 IntersectionObserver 滚动高亮
//
// Markdown 渲染为自写的安全极简渲染器（不引第三方库、不使用
// dangerouslySetInnerHTML，所有正文均为 React 文本节点，链接仅放行 http/https）。

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import {
  getReviewDetail,
  getReviewStatus,
  listReviewHistory,
  GEN_STATUS_LABELS,
  SCORE_GRADES,
  SECTION_TYPE_META,
  getScoreGrade,
  formatDuration,
  type ReviewDetail,
  type ReviewSection,
  type ReviewHistoryPage,
  type GenerationStatus,
  type ReviewLevel,
  type ScoreGrade,
} from "../services/review";
import { NpsSurvey } from "../components/NpsSurvey";
import {
  ChangeRecordSection,
  useChangeAnalysis,
} from "../components/ChangeRecordSection";

/* ===================== 常量 ===================== */

/** 状态轮询间隔与上限（70 次 ≈ 3.5 分钟） */
const POLL_INTERVAL_MS = 3000;
const MAX_POLLS = 70;
/** 长 Markdown 首屏挂载块数与每次追加步长（提前 600px 预加载） */
const INITIAL_BLOCKS = 20;
const BLOCK_STEP = 20;
/** 侧栏历史每页条数 */
const HISTORY_SIZE = 5;

const REVIEW_LEVEL_LABELS: Record<ReviewLevel, string> = {
  basic: "基础复盘",
  method: "方法论复盘",
  arch: "架构级复盘",
};

/* ===================== 主页面 ===================== */

type Phase = "loading" | "running" | "completed" | "failed" | "timeout";

interface NavItem {
  id: string;
  title: string;
  icon: string;
}

export function ReviewPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [detail, setDetail] = useState<ReviewDetail | null>(null);
  const [phase, setPhase] = useState<Phase>("loading");
  const [errorMsg, setErrorMsg] = useState("");
  const [reloadKey, setReloadKey] = useState(0);
  const [activeSection, setActiveSection] = useState("overview");
  const [readProgress, setReadProgress] = useState(0);
  const [showBackTop, setShowBackTop] = useState(false);
  const [growthProgress, setGrowthProgress] = useState<Pick<GrowthProgress,"phase"|"processed_sources"|"total_sources">>();

  const sectionRefs = useRef<Record<string, HTMLElement | null>>({});

  const reviewId = Number(id);

  /* ---------- 详情加载 + 状态轮询 ---------- */
  useEffect(() => {
    if (!Number.isInteger(reviewId) || reviewId <= 0) {
      setPhase("failed");
      setErrorMsg("复盘 ID 不合法");
      return;
    }

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let polls = 0;
    let growthDetected = false;
    const abort = new AbortController();

    const schedulePoll = (sessionId: number) => {
      polls += 1;
      if (polls > MAX_POLLS && !growthDetected) {
        if (!cancelled) setPhase((p) => (p === "running" ? "timeout" : p));
        return;
      }
      timer = setTimeout(async () => {
        if (cancelled) return;
        try {
          const status = await getReviewStatus(sessionId);
          if (cancelled) return;
          if(status.phase){growthDetected=true;setGrowthProgress({phase:status.phase,processed_sources:status.processed_sources||0,total_sources:status.total_sources||0});}
          if (status.generation_status === "completed") {
            await loadDetail();
          } else if (status.generation_status === "failed") {
            setPhase("failed");
            setErrorMsg(status.error_message || "复盘生成失败");
          } else {
            schedulePoll(sessionId);
          }
        } catch {
          // 单次轮询网络抖动不致命，按原节奏继续，直到轮询上限
          schedulePoll(sessionId);
        }
      }, POLL_INTERVAL_MS);
    };

    const loadDetail = async () => {
      const data = await getReviewDetail(reviewId);
      if (cancelled) return;
      setDetail(data);
      if (data.generation_status === "completed") {
        setPhase("completed");
      } else if (data.generation_status === "failed") {
        setPhase("failed");
        setErrorMsg(data.error_message || "复盘生成失败");
      } else {
        setPhase("running");
        try{const progress=await getGrowthProgress(reviewId,abort.signal);if(cancelled)return;growthDetected=true;setGrowthProgress(progress);}catch{if(cancelled)return;}
        schedulePoll(data.session_id);
      }
    };

    setPhase("loading");
    setGrowthProgress(undefined);
    setErrorMsg("");
    loadDetail().catch((e: unknown) => {
      if (cancelled) return;
      setPhase("failed");
      setErrorMsg(e instanceof Error ? e.message : "加载复盘报告失败，请稍后重试");
    });

    return () => {
      cancelled = true;
      abort.abort();
      if (timer) clearTimeout(timer);
    };
    // reloadKey 用于失败/超时后手动重试
  }, [reviewId, reloadKey]);

  /* ---------- 滚动：阅读进度 + 回到顶部（passive + rAF 节流） ---------- */
  useEffect(() => {
    let ticking = false;
    const update = () => {
      const scrollTop = window.scrollY;
      const docHeight = document.documentElement.scrollHeight - window.innerHeight;
      setReadProgress(docHeight > 0 ? Math.min((scrollTop / docHeight) * 100, 100) : 0);
      setShowBackTop(scrollTop > 400);
      ticking = false;
    };
    const onScroll = () => {
      if (ticking) return;
      ticking = true;
      requestAnimationFrame(update);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  /* ---------- 排序后的章节 ---------- */
  const sortedSections = useMemo<ReviewSection[]>(() => {
    if (!detail) return [];
    return [...detail.sections].sort((a, b) => a.sort_order - b.sort_order);
  }, [detail]);

  /* ---------- 动态目录 ---------- */
  const navItems = useMemo<NavItem[]>(() => {
    const items: NavItem[] = [{ id: "overview", title: "总评概览", icon: "🏆" }];
    if (phase === "completed") {
      sortedSections.forEach((s) => {
        items.push({
          id: `sec-${s.id}`,
          title: sectionTitle(s),
          icon: SECTION_TYPE_META[s.section_type]?.icon || "📄",
        });
      });
    }
    items.push({ id: "feedback", title: "报告反馈", icon: "📝" });
    return items;
  }, [phase, sortedSections]);

  /* ---------- 章节可见性高亮 ---------- */
  useEffect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        let visibleId = "";
        let maxRatio = 0;
        entries.forEach((entry) => {
          if (entry.isIntersecting && entry.intersectionRatio > maxRatio) {
            maxRatio = entry.intersectionRatio;
            visibleId = entry.target.id;
          }
        });
        if (visibleId) setActiveSection(visibleId);
      },
      { rootMargin: "-80px 0px -60% 0px", threshold: [0, 0.1, 0.25, 0.5] },
    );
    navItems.forEach((item) => {
      const el = sectionRefs.current[item.id];
      if (el) observer.observe(el);
    });
    return () => observer.disconnect();
  }, [navItems]);

  const scrollToSection = useCallback((sectionId: string) => {
    sectionRefs.current[sectionId]?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  const handleRetry = () => setReloadKey((k) => k + 1);

  /* ---------- 首次加载骨架屏 ---------- */
  if (phase === "loading") return <SkeletonPage />;
  if (!detail) {
    return <ErrorState message={errorMsg || "报告不存在"} onRetry={handleRetry} />;
  }

  const grade = getScoreGrade(detail.tech_score);

  return (
    <div className="min-h-screen bg-ink-50/50">
      {/* 顶部阅读进度条 */}
      <div className="fixed top-0 left-0 right-0 h-1 bg-transparent z-50">
        <div
          className="h-full bg-gradient-to-r from-brand-400 to-brand-600 transition-all duration-150 ease-out"
          style={{ width: `${readProgress}%` }}
        />
      </div>

      {/* 顶部面包屑 */}
      <div className="bg-white/80 backdrop-blur-sm border-b border-ink-100 sticky top-0 z-30">
        <div className="page-container h-14 flex items-center justify-between">
          <div className="flex items-center gap-2 text-[13px]">
            <Link to="/history" className="text-ink-400 hover:text-ink-600 transition-colors">
              复盘历史
            </Link>
            <span className="text-ink-300">/</span>
            <span className="text-ink-700 font-medium truncate max-w-[360px]">
              {detail.title || "复盘报告"}
            </span>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-[11px] text-ink-400">已读 {Math.round(readProgress)}%</span>
            <button
              onClick={() => navigate(-1)}
              className="px-3 py-1.5 text-[12px] text-ink-500 hover:text-ink-700 hover:bg-ink-100 rounded-lg transition-colors"
            >
              ← 返回
            </button>
          </div>
        </div>
      </div>

      <div className="page-container py-8 flex gap-6">
        {/* 左侧章节导航 */}
        <aside className="w-48 flex-shrink-0 hidden md:block">
          <div className="sticky top-24 space-y-4">
            <div className="bg-white rounded-xl border border-ink-100 p-3">
              <p className="text-[11px] font-semibold text-ink-400 mb-3 px-2">
                报告目录
              </p>
              <nav className="space-y-0.5">
                {navItems.map((item, idx) => (
                  <button
                    key={item.id}
                    onClick={() => scrollToSection(item.id)}
                    className={`w-full flex items-center gap-2 px-2.5 py-2 rounded-lg text-[13px] transition-all ${
                      activeSection === item.id
                        ? "bg-brand-50 text-brand-700 font-medium"
                        : "text-ink-500 hover:text-ink-700 hover:bg-ink-50"
                    }`}
                  >
                    <span className="text-base">{item.icon}</span>
                    <span className="flex-1 text-left truncate">{item.title}</span>
                    <span
                      className={`text-[10px] w-4 h-4 flex items-center justify-center rounded-full ${
                        activeSection === item.id
                          ? "bg-brand-500 text-white"
                          : "bg-ink-100 text-ink-400"
                      }`}
                    >
                      {idx + 1}
                    </span>
                  </button>
                ))}
              </nav>
            </div>

            {/* 报告元信息 */}
            <div className="bg-white rounded-xl border border-ink-100 p-4">
              <div className="text-[11px] font-bold text-ink-400 uppercase tracking-wider mb-3">
                报告信息
              </div>
              <div className="space-y-2.5">
                <InfoMini label="复盘级别" value={REVIEW_LEVEL_LABELS[detail.review_level] || detail.review_level} />
                <InfoMini label="会话 ID" value={String(detail.session_id)} />
                <InfoMini label="项目 ID" value={String(detail.project_id)} />
                <InfoMini label="生成模型" value={detail.ai_model || "—"} />
                <InfoMini label="重试次数" value={String(detail.retry_count ?? 0)} />
              </div>
            </div>
          </div>
        </aside>

        {/* 主内容区 */}
        <main className="flex-1 min-w-0 space-y-5">
          {phase === "completed" ? (
            <>
              {/* 总评概览 */}
              <section
                id="overview"
                ref={(el) => {
                  sectionRefs.current["overview"] = el;
                }}
                className="bg-white rounded-2xl border border-ink-100 p-6 md:p-8 scroll-mt-24"
              >
                <div className="flex items-start justify-between gap-4 mb-6 flex-wrap">
                  <div className="min-w-0">
                    <h1 className="text-xl font-bold text-ink-800 mb-2 break-words">
                      {detail.title || "复盘报告"}
                    </h1>
                    <div className="flex items-center gap-2 flex-wrap">
                      <StatusBadge status="completed" />
                      <span className="text-[11px] px-2 py-0.5 rounded-full bg-ink-100 text-ink-500">
                        {REVIEW_LEVEL_LABELS[detail.review_level] || detail.review_level}
                      </span>
                      <span className="text-[11px] text-ink-400">
                        生成于 {formatDateTime(detail.created_at)}
                      </span>
                    </div>
                  </div>
                </div>

                {/* 总分 + 标量指标 */}
                <div className="flex items-center gap-6 md:gap-8 mb-8 flex-wrap">
                  <ScoreRing score={detail.tech_score} grade={grade} />
                  <div className="flex-1 grid grid-cols-2 sm:grid-cols-4 gap-3 min-w-[260px]">
                    <StatCard label="技术分" value={fmtScore(detail.tech_score)} icon="🔧" color="text-brand-600" />
                    <StatCard label="协作分" value={detail.collab_score > 0 ? String(detail.collab_score) : "—"} icon="🤝" color="text-purple-600" />
                    <StatCard label="提问质量" value={fmtScore(detail.question_quality_score)} icon="💬" color="text-ink-700" />
                    <StatCard label="判断力" value={fmtScore(detail.judgment_score)} icon="🧭" color="text-ink-700" />
                    <StatCard label="错误识别率" value={fmtPercent(detail.error_recognition_rate)} icon="🎯" color="text-ok-600" />
                    <StatCard label="证据验证率" value={fmtPercent(detail.evidence_validation_rate)} icon="🔍" color="text-ok-600" />
                    <StatCard
                      label="坑点识别"
                      value={`${detail.ai_pitfall_recognized ?? 0}/${detail.ai_pitfall_count ?? 0}`}
                      icon="🎭"
                      color="text-warn-600"
                    />
                    <StatCard label="生成耗时" value={formatDuration(detail.generation_duration_ms)} icon="⏱️" color="text-ink-600" />
                  </div>
                </div>

                {/* 总结 */}
                {detail.summary && (
                  <div className="mb-6">
                    <h2 className="text-[15px] font-bold text-ink-800 mb-2 flex items-center gap-2">
                      <span>📝</span> 总结
                    </h2>
                    <MarkdownView content={detail.summary} />
                  </div>
                )}

                {/* 整体评价 */}
                {detail.overall_evaluation && (
                  <div className="p-4 bg-brand-50/40 rounded-xl border border-brand-100">
                    <h2 className="text-[15px] font-bold text-ink-800 mb-2 flex items-center gap-2">
                      <span>⭐</span> 整体评价
                    </h2>
                    <MarkdownView content={detail.overall_evaluation} />
                  </div>
                )}
              </section>

              {detail.growth_report && <GrowthReviewPanel key={detail.id} reviewId={detail.id} data={detail.growth_report} />}

              {/* AI 生成章节（按 sort_order） */}
              {sortedSections.map((section) => {
                const meta = SECTION_TYPE_META[section.section_type];
                return (
                  <section
                    key={section.id}
                    id={`sec-${section.id}`}
                    ref={(el) => {
                      sectionRefs.current[`sec-${section.id}`] = el;
                    }}
                    className="bg-white rounded-2xl border border-ink-100 p-6 md:p-8 scroll-mt-24"
                  >
                    <div className="mb-5">
                      <h2 className="text-lg font-bold text-ink-800 flex items-center gap-2">
                        <span>{meta?.icon || "📄"}</span>
                        {sectionTitle(section)}
                      </h2>
                    </div>
                    <SectionBody section={section} />
                  </section>
                );
              })}

              {sortedSections.length === 0 && (
                <div className="bg-white rounded-2xl border border-dashed border-ink-200 p-10 text-center text-[13px] text-ink-400">
                  本次复盘暂无章节内容
                </div>
              )}

              {/* 底部行动按钮 */}
              <div className="flex items-center justify-center gap-4 py-2">
                <button
                  onClick={() => navigate("/projects")}
                  className="px-6 py-2.5 bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium rounded-xl transition-colors"
                >
                  继续训练
                </button>
                <button
                  onClick={() => navigate("/history")}
                  className="px-6 py-2.5 border border-ink-200 text-ink-600 text-[13px] font-medium rounded-xl hover:bg-ink-50 transition-colors"
                >
                  查看历史
                </button>
              </div>
            </>
          ) : phase === "running" ? (
            <section
              id="overview"
              ref={(el) => {
                sectionRefs.current["overview"] = el;
              }}
              className="scroll-mt-24"
            >
              <BrainStatus sessionId={detail.session_id} />
              <GeneratingCard status={detail.generation_status} />
              {growthProgress && <div role="status" className="bg-white rounded-xl border border-ink-100 p-5 mt-4 text-sm">
                <p>{({analyze:"正在分析整场记录",reduce:"正在归并排查证据",synthesize:"正在形成成长报告",finalize:"正在保存报告",completed:"报告已完成",failed:"报告生成失败"} as Record<string,string>)[growthProgress.phase] || "正在准备复盘"}</p>
                <p className="text-ink-500 mt-2">已完整读取 {growthProgress.processed_sources} / {growthProgress.total_sources} 条来源</p>
              </div>}
            </section>
          ) : phase === "timeout" ? (
            <TimeoutCard onRetry={handleRetry} />
          ) : (
            <FailedCard message={errorMsg} onRetry={handleRetry} />
          )}

          {/* NPS 评价 */}
          <div
            id="feedback"
            ref={(el) => {
              sectionRefs.current["feedback"] = el;
            }}
            className="scroll-mt-24"
          >
            <BrainStatus sessionId={detail.session_id} />
            <NpsSurvey trainingId={detail.session_id} />
          </div>
        </main>

        {/* 右侧历史复盘（真分页） */}
        <aside className="w-60 flex-shrink-0 hidden xl:block">
          <HistorySidebar currentId={detail.id} />
        </aside>
      </div>

      {/* 回到顶部 */}
      <button
        onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
        className={`fixed bottom-8 right-8 w-10 h-10 rounded-full bg-white border border-ink-200 shadow-lg
          flex items-center justify-center text-ink-500 hover:text-brand-500 hover:border-brand-300
          transition-all duration-300 z-40
          ${showBackTop ? "opacity-100 translate-y-0" : "opacity-0 translate-y-4 pointer-events-none"}`}
        aria-label="回到顶部"
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 19V5" />
          <path d="M5 12l7-7 7 7" />
        </svg>
      </button>
    </div>
  );
}

/* ===================== 章节标题 ===================== */

function sectionTitle(s: ReviewSection): string {
  return s.title || SECTION_TYPE_META[s.section_type]?.title || s.section_type;
}

/* ===================== 章节正文（按类型分支渲染） ===================== */

const SECTION_TYPE_ASSISTANT_CHANGES = "assistant_changes";

function SectionBody({ section }: { section: ReviewSection }) {
  // Hooks 必须无条件调用：解析修改记录的结构化载荷（非该章节时返回 null）
  const changeAnalysis = useChangeAnalysis(section);

  // 助手修改记录：优先结构化卡片，content_data 缺失/非法时退回 Markdown
  if (
    section.section_type === SECTION_TYPE_ASSISTANT_CHANGES &&
    changeAnalysis
  ) {
    return <ChangeRecordSection section={section} />;
  }

  if (section.content) return <MarkdownView content={section.content} />;
  return <p className="text-[13px] text-ink-400">本章节内容暂缺</p>;
}

/* ===================== 数值格式化 ===================== */

function fmtScore(n: number): string {
  return n > 0 ? String(n) : "—";
}

function fmtPercent(n: number): string {
  return n > 0 ? `${n}%` : "—";
}

function formatDateTime(s: string): string {
  return formatIsoMinute(s);
}

/* ===================== 状态徽章 ===================== */

function StatusBadge({ status }: { status: GenerationStatus }) {
  const styles: Record<GenerationStatus, string> = {
    pending: "bg-brand-50 text-brand-700 border-brand-200",
    generating: "bg-brand-50 text-brand-700 border-brand-200",
    completed: "bg-ok-50 text-ok-700 border-ok-200",
    failed: "bg-danger-50 text-danger-700 border-danger-200",
  };
  const busy = status === "pending" || status === "generating";
  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full border text-[11px] font-medium ${styles[status]}`}
    >
      {busy && <span className="w-1.5 h-1.5 rounded-full bg-brand-500 animate-pulse" />}
      {GEN_STATUS_LABELS[status]}
    </span>
  );
}

/* ===================== 安全极简 Markdown 渲染器 ===================== */
/*
 * 不引第三方库、不使用 dangerouslySetInnerHTML：
 * 块级解析（代码围栏/标题/引用/列表/表格/分割线/段落），
 * 行内仅支持 **粗体**、*斜体*、`代码`、[文本](https?://链接)，
 * 链接白名单仅放行 http/https，其余一律按纯文本输出。
 */

type MdBlock =
  | { type: "code"; lang: string; text: string }
  | { type: "heading"; level: number; text: string }
  | { type: "hr" }
  | { type: "quote"; lines: string[] }
  | { type: "ul"; items: string[] }
  | { type: "ol"; items: string[] }
  | { type: "table"; head: string[]; rows: string[][] }
  | { type: "p"; text: string };

function isSpecialLine(line: string): boolean {
  return (
    line.trim() === "" ||
    /^#{1,4}\s/.test(line) ||
    /^```/.test(line) ||
    /^\s*[-*+]\s+/.test(line) ||
    /^\s*\d+\.\s+/.test(line) ||
    /^>/.test(line) ||
    /^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)
  );
}

function isTableSep(s: string): boolean {
  const t = s.trim();
  return t.includes("|") && t.includes("-") && /^[\s|:-]+$/.test(t);
}

function splitTableRow(row: string): string[] {
  return row
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());
}

export function parseMarkdown(md: string): MdBlock[] {
  const lines = md.replace(/\r\n/g, "\n").split("\n");
  const blocks: MdBlock[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) {
      i += 1;
      continue;
    }

    // 代码围栏（支持三或四反引号；修改记录章节的内容可能含三反引号，故用四反引号包裹）
    const fence = line.match(/^(`{3,4})(\w*)\s*$/);
    if (fence) {
      const closeRe = new RegExp(`^${fence[1]}\\s*$`);
      const buf: string[] = [];
      i += 1;
      while (i < lines.length && !closeRe.test(lines[i])) {
        buf.push(lines[i]);
        i += 1;
      }
      i += 1; // 跳过结束围栏（缺失也安全结束）
      blocks.push({ type: "code", lang: fence[2] || "", text: buf.join("\n") });
      continue;
    }

    // 标题
    const h = line.match(/^(#{1,4})\s+(.*)$/);
    if (h) {
      blocks.push({ type: "heading", level: h[1].length, text: h[2].trim() });
      i += 1;
      continue;
    }

    // 分割线
    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      blocks.push({ type: "hr" });
      i += 1;
      continue;
    }

    // 引用
    if (/^>\s?/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^>\s?/, ""));
        i += 1;
      }
      blocks.push({ type: "quote", lines: buf });
      continue;
    }

    // 无序列表
    if (/^\s*[-*+]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*+]\s+/, ""));
        i += 1;
      }
      blocks.push({ type: "ul", items });
      continue;
    }

    // 有序列表
    if (/^\s*\d+\.\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+\.\s+/, ""));
        i += 1;
      }
      blocks.push({ type: "ol", items });
      continue;
    }

    // 表格（当前行含 | 且下一行为分隔行）
    if (line.includes("|") && i + 1 < lines.length && isTableSep(lines[i + 1])) {
      const head = splitTableRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes("|") && lines[i].trim()) {
        rows.push(splitTableRow(lines[i]));
        i += 1;
      }
      blocks.push({ type: "table", head, rows });
      continue;
    }

    // 普通段落（聚合连续非特殊行）
    const buf: string[] = [];
    while (i < lines.length && !isSpecialLine(lines[i])) {
      buf.push(lines[i]);
      i += 1;
    }
    blocks.push({ type: "p", text: buf.join(" ").trim() });
  }

  return blocks;
}

/** 行内解析：粗体 / 斜体 / 行内代码 / 安全链接（仅 http/https） */
function renderInline(text: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const regex =
    /(`[^`\n]+`)|(\*\*([^*\n]+)\*\*)|(\*([^*\n]+)\*|_([^_\n]+)_)|(\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\))/g;
  let lastIndex = 0;
  let key = 0;
  let m: RegExpExecArray | null;

  while ((m = regex.exec(text)) !== null) {
    if (m.index > lastIndex) nodes.push(text.slice(lastIndex, m.index));
    if (m[1]) {
      nodes.push(
        <code key={key++} className="px-1 py-0.5 rounded bg-ink-100 text-[12px] font-mono text-brand-700">
          {m[1].slice(1, -1)}
        </code>,
      );
    } else if (m[2]) {
      nodes.push(
        <strong key={key++} className="font-semibold text-ink-800">
          {m[3]}
        </strong>,
      );
    } else if (m[4]) {
      nodes.push(<em key={key++}>{m[5] ?? m[6]}</em>);
    } else if (m[7]) {
      nodes.push(
        <a
          key={key++}
          href={m[9]}
          target="_blank"
          rel="noopener noreferrer"
          className="text-brand-600 underline underline-offset-2 hover:text-brand-700"
        >
          {m[8]}
        </a>,
      );
    }
    lastIndex = regex.lastIndex;
  }
  if (lastIndex < text.length) nodes.push(text.slice(lastIndex));
  return nodes;
}

/** Markdown 块渲染（长内容：首屏 20 块 + 哨兵增量 + content-visibility） */
function MarkdownView({ content }: { content: string }) {
  const blocks = useMemo(() => parseMarkdown(content), [content]);
  const [count, setCount] = useState(INITIAL_BLOCKS);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  // 内容切换（如轮询完成后重新拉取）时重置增量计数
  useEffect(() => {
    setCount(INITIAL_BLOCKS);
  }, [content]);

  useEffect(() => {
    if (count >= blocks.length) return;
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          setCount((c) => Math.min(c + BLOCK_STEP, blocks.length));
        }
      },
      { rootMargin: "600px 0px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [count, blocks.length]);

  const visible = blocks.slice(0, count);
  const remaining = blocks.length - visible.length;

  return (
    <div className="space-y-3">
      {visible.map((b, idx) => (
        <div
          key={idx}
          style={{ contentVisibility: "auto", containIntrinsicSize: "auto 160px" }}
        >
          <BlockView block={b} />
        </div>
      ))}
      {remaining > 0 && (
        <div ref={sentinelRef} className="py-3 flex flex-col items-center gap-2">
          <div className="h-5 w-5 border-2 border-ink-200 border-t-brand-500 rounded-full animate-spin" />
          <button
            type="button"
            onClick={() => setCount((c) => Math.min(c + BLOCK_STEP, blocks.length))}
            className="text-[12px] text-brand-600 hover:text-brand-700"
          >
            展开剩余 {remaining} 段
          </button>
        </div>
      )}
    </div>
  );
}

function BlockView({ block }: { block: MdBlock }) {
  switch (block.type) {
    case "code":
      return (
        <pre className="bg-ink-900 text-ink-100 rounded-xl p-4 overflow-x-auto text-[12.5px] leading-6 font-mono">
          {block.lang && (
            <div className="text-[10px] text-ink-400 mb-2 select-none">{block.lang}</div>
          )}
          <code>{block.text}</code>
        </pre>
      );
    case "heading": {
      const Tag = (`h${Math.min(block.level + 2, 5)}`) as "h3" | "h4" | "h5";
      const cls =
        block.level === 1
          ? "text-[16px] font-bold text-ink-800 mt-2"
          : block.level === 2
            ? "text-[14px] font-semibold text-ink-800"
            : "text-[13px] font-semibold text-ink-600";
      return <Tag className={cls}>{renderInline(block.text)}</Tag>;
    }
    case "hr":
      return <hr className="my-4 border-ink-100" />;
    case "quote":
      return (
        <blockquote className="border-l-4 border-brand-200 bg-brand-50/40 rounded-r-lg pl-4 pr-3 py-2 space-y-1">
          {block.lines.map((ln, i) => (
            <p key={i} className="text-[13px] leading-7 text-ink-600">
              {renderInline(ln)}
            </p>
          ))}
        </blockquote>
      );
    case "ul":
      return (
        <ul className="list-disc pl-5 space-y-1.5">
          {block.items.map((it, i) => (
            <li key={i} className="text-[14px] leading-7 text-ink-700 marker:text-ink-400">
              {renderInline(it)}
            </li>
          ))}
        </ul>
      );
    case "ol":
      return (
        <ol className="list-decimal pl-5 space-y-1.5">
          {block.items.map((it, i) => (
            <li key={i} className="text-[14px] leading-7 text-ink-700 marker:text-ink-400">
              {renderInline(it)}
            </li>
          ))}
        </ol>
      );
    case "table":
      return (
        <div className="overflow-x-auto rounded-xl border border-ink-100">
          <table className="w-full text-[13px] border-collapse">
            <thead>
              <tr className="bg-ink-50">
                {block.head.map((h, i) => (
                  <th key={i} className="border-b border-ink-100 px-3 py-2 text-left font-semibold text-ink-700 whitespace-nowrap">
                    {renderInline(h)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, ri) => (
                <tr key={ri} className="even:bg-ink-50/40">
                  {row.map((cell, ci) => (
                    <td key={ci} className="border-b border-ink-50 px-3 py-2 text-ink-600 align-top">
                      {renderInline(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "p":
      return <p className="text-[14px] leading-7 text-ink-700">{renderInline(block.text)}</p>;
    default:
      return null;
  }
}

/* ===================== 指标卡 / 总分环 ===================== */

function StatCard({
  label,
  value,
  icon,
  color,
}: {
  label: string;
  value: string;
  icon: string;
  color: string;
}) {
  return (
    <div className="p-3 bg-ink-50/50 rounded-xl border border-ink-100">
      <div className="flex items-center gap-1.5 mb-1">
        <span>{icon}</span>
        <span className="text-[11px] text-ink-500">{label}</span>
      </div>
      <div className={`text-[18px] font-bold ${color}`}>{value}</div>
    </div>
  );
}

function ScoreRing({ score, grade }: { score: number; grade: ScoreGrade | null }) {
  const gradeConfig = grade ? SCORE_GRADES[grade] : null;
  const colorMap: Record<ScoreGrade, string> = {
    S: "#6366f1",
    A: "#10b981",
    B: "#34d399",
    C: "#f59e0b",
    D: "#ef4444",
  };
  const color = grade ? colorMap[grade] : "#94a3b8";
  const circumference = 326.7;
  const dasharray = `${(Math.max(score, 0) / 100) * circumference} ${circumference}`;

  return (
    <div className="relative w-32 h-32 flex-shrink-0">
      <svg className="w-full h-full -rotate-90" viewBox="0 0 120 120">
        <circle cx="60" cy="60" r="52" fill="none" stroke="#f1f5f9" strokeWidth="10" />
        <circle
          cx="60"
          cy="60"
          r="52"
          fill="none"
          stroke={color}
          strokeWidth="10"
          strokeLinecap="round"
          strokeDasharray={dasharray}
          className="transition-all duration-1000 ease-out"
        />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-4xl font-bold" style={{ color }}>
          {score > 0 ? score : "—"}
        </span>
        <span className="text-[12px] text-ink-400 mt-0.5">
          {gradeConfig ? gradeConfig.label : "暂无评分"}
        </span>
      </div>
    </div>
  );
}

function InfoMini({
  label,
  value,
  valueClass = "text-ink-700",
}: {
  label: string;
  value: string;
  valueClass?: string;
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <span className="text-[11px] text-ink-400 flex-shrink-0">{label}</span>
      <span className={`text-[12px] font-medium truncate ${valueClass}`} title={value}>
        {value}
      </span>
    </div>
  );
}

/* ===================== 生成中 / 超时 / 失败 ===================== */

function GeneratingCard({ status }: { status: GenerationStatus }) {
  const [secs, setSecs] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setSecs((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="bg-white rounded-2xl border border-ink-100 p-10 md:p-16 text-center">
      <div className="w-12 h-12 mx-auto mb-5 border-[3px] border-brand-200 border-t-brand-500 rounded-full animate-spin" />
      <div className="text-[16px] font-semibold text-ink-800 mb-2">
        AI 正在{GEN_STATUS_LABELS[status]}复盘报告…
      </div>
      <p className="text-[13px] text-ink-500">
        报告会在后台继续生成。此页会自动更新状态，你也可以离开，稍后从「我的训练」查看。
      </p>
      <p className="text-[12px] text-ink-400 mt-2">已等待 {secs} 秒</p>
    </div>
  );
}

function TimeoutCard({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="bg-white rounded-2xl border border-warn-200 p-10 md:p-16 text-center">
      <div className="text-4xl mb-4">⏳</div>
      <div className="text-[16px] font-semibold text-ink-800 mb-2">生成时间超出预期</div>
      <p className="text-[13px] text-ink-500 mb-6">
        复盘仍在后台生成中，可稍后手动刷新状态；长时间未完成请联系管理员排查生成任务。
      </p>
      <button
        onClick={onRetry}
        className="px-6 py-2.5 bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium rounded-xl transition-colors"
      >
        刷新状态
      </button>
    </div>
  );
}

function FailedCard({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="bg-white rounded-2xl border border-danger-200 p-10 md:p-16 text-center">
      <div className="text-4xl mb-4">😔</div>
      <div className="text-[16px] font-semibold text-ink-800 mb-2">复盘生成失败</div>
      {message && <p className="text-[13px] text-danger-600 mb-6 break-words">{message}</p>}
      <button
        onClick={onRetry}
        className="px-6 py-2.5 bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium rounded-xl transition-colors"
      >
        重新加载
      </button>
    </div>
  );
}

/* ===================== 右侧历史复盘（真分页） ===================== */

function HistorySidebar({ currentId }: { currentId: number }) {
  const [page, setPage] = useState(1);
  const [data, setData] = useState<ReviewHistoryPage | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    listReviewHistory({ page, size: HISTORY_SIZE })
      .then((d) => {
        if (!cancelled) setData(d);
      })
      .catch(() => {
        if (!cancelled) setData(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [page]);

  const totalPages = data ? Math.max(1, Math.ceil(data.total / HISTORY_SIZE)) : 1;

  return (
    <div className="sticky top-24">
      <div className="bg-white rounded-xl border border-ink-100 p-4">
        <div className="text-[11px] font-bold text-ink-400 uppercase tracking-wider mb-3">
          历史复盘
        </div>

        {loading ? (
          <div className="space-y-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className="h-14 bg-ink-50 rounded-lg animate-pulse" />
            ))}
          </div>
        ) : !data || data.list.length === 0 ? (

          <p className="text-[12px] text-ink-400 py-4 text-center">暂无历史复盘</p>
        ) : (
          <>
            <div className="space-y-2">
              {data.list.map((item) => (
                <HistoryItem key={item.id} item={item} active={item.id === currentId} />
              ))}
            </div>

            {totalPages > 1 && (
              <div className="flex items-center justify-between mt-3 pt-3 border-t border-ink-100">
                <button
                  type="button"
                  disabled={page === 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  className="text-[11px] text-ink-500 hover:text-brand-600 disabled:opacity-30 disabled:cursor-not-allowed"
                >
                  ← 上一页
                </button>
                <span className="text-[11px] text-ink-400">
                  {page} / {totalPages}
                </span>
                <button
                  type="button"
                  disabled={page === totalPages}
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  className="text-[11px] text-ink-500 hover:text-brand-600 disabled:opacity-30 disabled:cursor-not-allowed"
                >
                  下一页 →
                </button>
              </div>
            )}
          </>
        )}

        <Link
          to="/history"
          className="block mt-3 text-center text-[12px] text-brand-600 hover:text-brand-700 font-medium"
        >
          查看全部 →
        </Link>
      </div>
    </div>
  );
}

function HistoryItem({
  item,
  active,
}: {
  item: ReviewHistoryPage["list"][number];
  active: boolean;
}) {
  return (
    <Link
      to={`/review/${item.id}`}
      className={`block p-3 rounded-lg border transition-all ${
        active
          ? "border-brand-300 bg-brand-50/50"
          : "border-ink-100 hover:border-ink-200 hover:bg-ink-50"
      }`}
    >
      <div className="flex items-center justify-between gap-2 mb-1">
        <span className="text-[12px] font-semibold text-ink-700 truncate">
          {item.title || "未命名复盘"}
        </span>
        {item.generation_status === "completed" ? (
          item.tech_score > 0 && (
            <span className={`text-[11px] font-bold flex-shrink-0 ${scoreColor(item.tech_score)}`}>
              {item.tech_score}
            </span>
          )
        ) : (
          <span className="flex-shrink-0">
            <StatusBadge status={item.generation_status} />
          </span>
        )}
      </div>
      <div className="flex items-center gap-1.5 text-[10px] text-ink-400">
        <span>{formatDateTime(item.created_at)}</span>
        <span>·</span>
        <span>{formatDuration(item.generation_duration_ms)}</span>
      </div>
    </Link>
  );
}

function scoreColor(score: number): string {
  if (score >= 90) return "text-brand-600";
  if (score >= 80) return "text-ok-600";
  if (score >= 70) return "text-ok-500";
  if (score >= 60) return "text-warn-600";
  return "text-danger-600";
}

/* ===================== 骨架屏 / 全屏失败态 ===================== */

function SkeletonPage() {
  return (
    <div className="min-h-screen bg-ink-50/50">
      <div className="fixed top-0 left-0 right-0 h-1 bg-transparent z-50 overflow-hidden">
        <div className="h-full w-1/3 bg-gradient-to-r from-transparent via-brand-400 to-transparent animate-pulse" />
      </div>
      <div className="bg-white border-b border-ink-100 h-14" />

      <div className="page-container py-8 flex gap-6">
        {/* 左侧章节导航 */}
        <aside className="w-48 flex-shrink-0 hidden md:block">
          <div className="sticky top-24 space-y-4">
            <div className="bg-white rounded-xl border border-ink-100 p-3">
              <div className="h-3 w-16 bg-ink-100 rounded mb-3 mx-2" />
              <div className="space-y-2">
                {Array.from({ length: 4 }).map((_, i) => (
                  <div key={i} className="flex items-center gap-2 px-2.5 py-2">
                    <div className="w-5 h-5 bg-ink-100 rounded" />
                    <div className="h-3 flex-1 bg-ink-100 rounded" />
                  </div>
                ))}
              </div>
            </div>
          </div>
        </aside>

        <main className="flex-1 min-w-0 space-y-5">
          <div className="bg-white rounded-2xl border border-ink-100 p-6 md:p-8">
            <div className="h-6 w-48 bg-ink-100 rounded-lg animate-pulse mb-4" />
            <div className="flex items-center gap-8 mb-8">
              <div className="w-32 h-32 rounded-full bg-ink-100 animate-pulse" />
              <div className="flex-1 grid grid-cols-2 sm:grid-cols-4 gap-3">
                {Array.from({ length: 8 }).map((_, i) => (
                  <div key={i} className="h-16 bg-ink-50 rounded-xl animate-pulse" />
                ))}
              </div>
            </div>
            <div className="space-y-2">
              <div className="h-3 w-full bg-ink-100 rounded animate-pulse" />
              <div className="h-3 w-5/6 bg-ink-100 rounded animate-pulse" />
              <div className="h-3 w-4/6 bg-ink-100 rounded animate-pulse" />
            </div>
          </div>
          <div className="bg-white rounded-2xl border border-ink-100 p-6 md:p-8">
            <div className="space-y-3">
              {Array.from({ length: 4 }).map((_, i) => (
                <div key={i} className="h-4 bg-ink-50 rounded animate-pulse" style={{ animationDelay: `${i * 100}ms` }} />
              ))}
            </div>
          </div>
        </main>

        <aside className="w-60 flex-shrink-0 hidden xl:block">
          <div className="sticky top-24">
            <div className="bg-white rounded-xl border border-ink-100 p-4">
              <div className="h-3 w-16 bg-ink-100 rounded mb-4" />
              <div className="space-y-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <div key={i} className="h-14 bg-ink-50 rounded-lg animate-pulse" />
                ))}
              </div>
            </div>
          </div>
        </aside>
      </div>
    </div>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="min-h-screen bg-ink-50/50 flex items-center justify-center">
      <div className="text-center max-w-sm">
        <div className="text-5xl mb-4">😔</div>
        <div className="text-[16px] text-ink-700 font-semibold mb-2">加载失败</div>
        <div className="text-[13px] text-ink-500 mb-6 break-words">{message}</div>
        <button
          onClick={onRetry}
          className="px-6 py-2.5 bg-brand-500 hover:bg-brand-600 text-white text-[13px] font-medium rounded-xl transition-colors"
        >
          重新加载
        </button>
      </div>
    </div>
  );
}
