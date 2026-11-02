import { useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { tokenStore } from "@quenching/shared";
import { RiskWarningModal } from "../components/RiskWarningModal";
import { SandboxLaunchModal } from "../components/SandboxLaunchModal";
import {
  getProject,
  listProjectServices,
  type Project,
  type ProjectServiceItem,
  DOMAIN_LABELS,
  DOMAIN_ICONS,
  DIFFICULTY_LABELS,
  DIFFICULTY_COLORS,
  SERVICE_TYPE_LABELS,
  SERVICE_TYPE_ICONS,
  SERVICE_TYPE_COLORS,
  inferDifficulty,
  estimateDuration,
  getTrainingOutcomes,
} from "@quenching/shared";
import {
  DEFECT_CATEGORY_LABELS,
  DEFECT_CATEGORY_ICONS,
  DEFECT_DIFFICULTY_LABELS,
  DEFECT_DIFFICULTY_COLORS,
  formatFixTime,
  type DefectCategory,
  type DefectDifficulty,
} from "@quenching/shared";
import { fetchProjectDefects, type ProjectDefect } from "../services/defects";
import {
  ackRiskNotice,
} from "../services/profile";
import { createSession } from "../services/session";
import { createSandbox, waitUntilReady, type SandboxProgress } from "../services/sandbox";

/**
 * 项目详情页（T2-8 项目详情与训练）
 * - 项目基本信息（名称、领域、难度、技术栈、时长）
 * - 训练介绍与收获
 * - 架构服务展示（微服务列表）
 * - 开始训练按钮（MVP 跳登录占位）
 */
export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const projectId = Number(id);

  const [project, setProject] = useState<Project | null>(null);
  const [services, setServices] = useState<ProjectServiceItem[]>([]);
  const [defects, setDefects] = useState<ProjectDefect[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>("");
  const [riskModalOpen, setRiskModalOpen] = useState(false);
  const [riskConfirming, setRiskConfirming] = useState(false);
  const [riskError, setRiskError] = useState<string>("");
  const [launchModalOpen, setLaunchModalOpen] = useState(false);
  const [startConfirming, setStartConfirming] = useState(false);
  /** 沙箱启动进度（0-100），创建会话后展示 */
  const [sandboxPercent, setSandboxPercent] = useState(0);
  const [sandboxStage, setSandboxStage] = useState("");
  const [startError, setStartError] = useState("");

  // 并行加载项目详情 + 服务列表
  useEffect(() => {
    if (!projectId || Number.isNaN(projectId)) {
      setError("项目ID无效");
      setLoading(false);
      return;
    }

    setLoading(true);
    setError("");

    Promise.all([
      getProject(projectId).catch((e) => {
        throw new Error(e?.message ?? "加载项目详情失败");
      }),
      listProjectServices(projectId).catch(() => [] as ProjectServiceItem[]),
      fetchProjectDefects(projectId).catch(() => [] as ProjectDefect[]),
    ])
      .then(([proj, svcs, defs]) => {
        setProject(proj);
        setServices(svcs);
        setDefects(defs);
      })
      .catch((e) => {
        setError(e?.message ?? "加载失败，请稍后重试");
      })
      .finally(() => setLoading(false));
  }, [projectId]);

  const difficulty = project ? inferDifficulty(project.tech_stack) : "easy";
  const duration = estimateDuration(difficulty);
  const outcomes = getTrainingOutcomes(difficulty);

  // 点击开始训练：公开页可浏览，但动作需要登录；登录后回跳本页继续
  const handleStartTraining = () => {
    if (!tokenStore.get()) {
      navigate("/login", { state: { from: `/projects/${projectId}` } });
      return;
    }
    setRiskModalOpen(true);
  };

  // 风险告知确认后：调 risk_notice_ack，随后由系统判定起始难度并直接拉起沙箱
  const handleRiskConfirm = async () => {
    setRiskConfirming(true);
    setRiskError("");
    try {
      // 上报风险告知确认（后端幂等）；失败则保留弹窗，允许用户重试
      await ackRiskNotice();

      setRiskModalOpen(false);
      setLaunchModalOpen(true);
      await startTraining();
    } catch (e) {
      setRiskError(e instanceof Error ? e.message : "确认失败，请稍后重试");
    } finally {
      setRiskConfirming(false);
    }
  };

  // 创建会话 → 创建沙箱 → 轮询至 ready 才跳转训练页
  const startTraining = async () => {
    setStartConfirming(true);
    setStartError("");
    setSandboxPercent(0);
    setSandboxStage("");
    try {
      // 双重保险：动作前已校验登录，这里再判一次（会话可能在此期间过期）
      if (!tokenStore.get()) {
        navigate("/login", { state: { from: `/projects/${projectId}` } });
        return;
      }

      // 1) 创建训练会话（难度不传，由系统按用户能力实时判定）
      const sess = await createSession({ project_id: projectId });

      // 2) 创建沙箱（session_id + project_id 必填）
      await createSandbox({ session_id: sess.id, project_id: projectId });

      // 3) 轮询沙箱进度，必须 ready 才进入；failed/超时给出重试提示
      setSandboxStage("初始化中");
      const progress = await waitUntilReady(sess.id, (p: SandboxProgress) => {
        setSandboxPercent(p.percent);
        setSandboxStage(p.stage_name || p.message || "启动中");
      });

      // 4) ready，携带会话与沙箱标识进入实训工作台
      setSandboxPercent(progress.percent);
      navigate(`/training/${projectId}?session=${sess.id}&sandbox=${sess.id}`);
    } catch (e) {
      // 未就绪一律不跳转，保留弹窗并允许重试
      setStartError(e instanceof Error ? e.message : "启动失败，请稍后重试");
    } finally {
      setStartConfirming(false);
    }
  };

  return (
    <div className="bg-ink-50">
      <main className="page-container py-10">
        {/* 面包屑 */}
        <div className="mb-6 flex items-center gap-2 text-[13px] text-ink-400">
          <Link to="/projects" className="hover:text-ink-600 transition-colors">
            项目大厅
          </Link>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m9 18 6-6-6-6" />
          </svg>
          <span className="text-ink-600 font-medium truncate">
            {loading ? "加载中..." : project?.name ?? "项目详情"}
          </span>
        </div>

        {/* 加载中 */}
        {loading && (
          <div className="space-y-6 animate-pulse">
            <div className="bg-white rounded-2xl border border-ink-100 p-8">
              <div className="h-6 bg-ink-100 rounded w-24 mb-4" />
              <div className="h-10 bg-ink-100 rounded w-1/2 mb-4" />
              <div className="h-4 bg-ink-100 rounded w-3/4 mb-6" />
              <div className="flex gap-2 mb-6">
                <div className="h-6 bg-ink-100 rounded w-16" />
                <div className="h-6 bg-ink-100 rounded w-20" />
              </div>
              <div className="h-12 bg-ink-100 rounded-xl w-40" />
            </div>
            <div className="grid md:grid-cols-3 gap-6">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="bg-white rounded-2xl border border-ink-100 p-6 h-40" />
              ))}
            </div>
          </div>
        )}

        {/* 错误态 */}
        {!loading && error && (
          <div className="text-center py-24">
            <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-danger/10 flex items-center justify-center">
              <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-danger">
                <circle cx="12" cy="12" r="10" />
                <line x1="12" y1="8" x2="12" y2="12" />
                <line x1="12" y1="16" x2="12.01" y2="16" />
              </svg>
            </div>
            <h3 className="text-xl font-bold text-ink-700 mb-2">加载失败</h3>
            <p className="text-ink-500 mb-6">{error}</p>
            <div className="flex items-center justify-center gap-3">
              <Link
                to="/projects"
                className="h-10 px-5 rounded-lg border border-ink-200 bg-white hover:bg-ink-50 text-ink-700 font-medium text-[14px] transition-colors"
              >
                返回列表
              </Link>
              <button
                type="button"
                onClick={() => window.location.reload()}
                className="h-10 px-5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white font-medium text-[14px] transition-colors"
              >
                重新加载
              </button>
            </div>
          </div>
        )}

        {/* 详情内容 */}
        {!loading && !error && project && (
          <div className="grid lg:grid-cols-3 gap-8">
            {/* 左侧主内容 */}
            <div className="lg:col-span-2 space-y-6">
              {/* 项目头部卡片 */}
              <div className="bg-white rounded-2xl border border-ink-100 overflow-hidden">
                <div className="h-40 bg-gradient-to-br from-brand-50 via-white to-ai/10 relative">
                  <div className="absolute inset-0 flex items-center justify-center">
                    <span className="text-7xl opacity-20">
                      {DOMAIN_ICONS[project.domain]}
                    </span>
                  </div>
                </div>
                <div className="p-7">
                  <div className="flex flex-wrap items-center gap-2 mb-3">
                    <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-ink-50 text-ink-600 text-[12px] font-medium border border-ink-100">
                      <span>{DOMAIN_ICONS[project.domain]}</span>
                      {DOMAIN_LABELS[project.domain]}
                    </span>
                    <span
                      className={`inline-flex items-center px-2.5 py-1 rounded-full text-[12px] font-semibold border ${DIFFICULTY_COLORS[difficulty]}`}
                    >
                      {DIFFICULTY_LABELS[difficulty]}
                    </span>
                  </div>

                  <h1 className="text-2xl md:text-3xl font-bold text-ink-900 mb-3">
                    {project.name}
                  </h1>

                  <p className="text-ink-500 text-[15px] leading-relaxed mb-6">
                    本项目基于真实{DOMAIN_LABELS[project.domain]}架构搭建，包含完整的微服务体系与中间件。
                    在训练中，系统将自动注入生产级故障，你需要在 AI 助教的辅助下排查并修复问题，
                    同时警惕 AI 可能给出的错误建议。
                  </p>

                  {/* 技术栈 */}
                  <div className="mb-6">
                    <div className="text-[13px] font-semibold text-ink-700 mb-2">技术栈</div>
                    <div className="flex flex-wrap gap-2">
                      {project.tech_stack.map((tech) => (
                        <span
                          key={tech}
                          className="px-3 py-1 rounded-lg bg-ink-50 text-ink-600 text-[12px] font-medium border border-ink-100"
                        >
                          {tech}
                        </span>
                      ))}
                    </div>
                  </div>

                  {/* 开始训练按钮 */}
                  <button
                    type="button"
                    onClick={handleStartTraining}
                    className="h-12 px-8 rounded-xl bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[15px] inline-flex items-center gap-2 transition-all shadow-lg shadow-brand-500/20 hover:shadow-xl hover:shadow-brand-500/30 hover:-translate-y-0.5"
                  >
                    开始训练
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <polygon points="5 3 19 12 5 21 5 3" />
                    </svg>
                  </button>
                </div>
              </div>

              {/* 训练收获 */}
              <div className="bg-white rounded-2xl border border-ink-100 p-7">
                <h2 className="text-lg font-bold text-ink-800 mb-5 flex items-center gap-2">
                  <span className="w-8 h-8 rounded-lg bg-ok/10 text-ok flex items-center justify-center">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M20 6 9 17l-5-5" />
                    </svg>
                  </span>
                  训练收获
                </h2>
                <ul className="space-y-3">
                  {outcomes.map((item, i) => (
                    <li key={i} className="flex items-start gap-3">
                      <span className="w-5 h-5 rounded-full bg-ok/10 text-ok flex items-center justify-center flex-none mt-0.5">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                          <path d="M20 6 9 17l-5-5" />
                        </svg>
                      </span>
                      <span className="text-[14px] text-ink-600 leading-relaxed">{item}</span>
                    </li>
                  ))}
                </ul>
              </div>

              {/* 架构服务 */}
              {services.length > 0 && (
                <div className="bg-white rounded-2xl border border-ink-100 p-7">
                  <h2 className="text-lg font-bold text-ink-800 mb-5 flex items-center gap-2">
                    <span className="w-8 h-8 rounded-lg bg-ai/10 text-ai flex items-center justify-center">
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <rect width="7" height="7" x="3" y="3" rx="1" />
                        <rect width="7" height="7" x="14" y="3" rx="1" />
                        <rect width="7" height="7" x="14" y="14" rx="1" />
                        <rect width="7" height="7" x="3" y="14" rx="1" />
                      </svg>
                    </span>
                    架构组成
                    <span className="ml-1 px-2 py-0.5 rounded-full bg-ink-100 text-ink-500 text-[11px] font-medium">
                      {services.length} 个服务
                    </span>
                  </h2>
                  <div className="grid sm:grid-cols-2 gap-3">
                    {services.map((svc) => (
                      <div
                        key={svc.id}
                        className="flex items-center gap-3 p-4 rounded-xl bg-ink-50 border border-ink-100 hover:border-ink-200 transition-colors"
                      >
                        <span
                          className={`w-10 h-10 rounded-lg flex items-center justify-center text-lg border ${SERVICE_TYPE_COLORS[svc.service_type]}`}
                        >
                          {SERVICE_TYPE_ICONS[svc.service_type]}
                        </span>
                        <div className="flex-1 min-w-0">
                          <div className="font-semibold text-ink-800 text-[14px] truncate">
                            {svc.display_name}
                          </div>
                          <div className="text-[12px] text-ink-500 flex items-center gap-2">
                            <span>{SERVICE_TYPE_LABELS[svc.service_type]}</span>
                            <span className="text-ink-300">·</span>
                            <span className="truncate">{svc.tech_stack}</span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* 故障剧本 */}
              {defects.length > 0 && (
                <div className="bg-white rounded-2xl border border-ink-100 p-7">
                  <h2 className="text-lg font-bold text-ink-800 mb-5 flex items-center gap-2">
                    <span className="w-8 h-8 rounded-lg bg-danger-50 text-danger-500 flex items-center justify-center">
                      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
                        <line x1="12" x2="12" y1="9" y2="13" />
                        <line x1="12" x2="12.01" y1="17" y2="17" />
                      </svg>
                    </span>
                    故障剧本
                    <span className="ml-1 px-2 py-0.5 rounded-full bg-ink-100 text-ink-500 text-[11px] font-medium">
                      {defects.length} 个故障
                    </span>
                  </h2>
                  <div className="space-y-3">
                    {defects.map((defect) => (
                      <div
                        key={defect.id}
                        className="p-4 rounded-xl border border-ink-100 bg-ink-50/50 hover:border-ink-200 hover:bg-white transition-all"
                      >
                        <div className="flex items-start justify-between gap-3 mb-2">
                          <div className="flex items-center gap-2">
                            <span className="text-lg">{DEFECT_CATEGORY_ICONS[defect.category as DefectCategory]}</span>
                            <span className="font-semibold text-ink-800 text-[14px]">
                              {defect.name}
                            </span>
                          </div>
                          <div className="flex items-center gap-2 flex-shrink-0">
                            <span
                              className={`px-2 py-0.5 text-[10px] font-bold rounded-full border ${DEFECT_DIFFICULTY_COLORS[defect.difficulty as DefectDifficulty]}`}
                            >
                              {DEFECT_DIFFICULTY_LABELS[defect.difficulty as DefectDifficulty]}
                            </span>
                          </div>
                        </div>
                        <p className="text-[12px] text-ink-500 mb-3 line-clamp-2">
                          {defect.description}
                        </p>
                        <div className="flex items-center gap-4 text-[11px] text-ink-400">
                          <span className="flex items-center gap-1">
                            <span>🏷️</span>
                            {DEFECT_CATEGORY_LABELS[defect.category as DefectCategory]}
                          </span>
                          <span className="flex items-center gap-1">
                            <span>⏱️</span>
                            预计 {formatFixTime(defect.estimated_fix_time)}
                          </span>
                          <span className="flex items-center gap-1">
                            <span>🎯</span>
                            {defect.score_weight.toFixed(1)}x 权重
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>

            {/* 右侧侧边栏 */}
            <div className="space-y-6">
              {/* 训练信息卡片 */}
              <div className="bg-white rounded-2xl border border-ink-100 p-6 sticky top-24">
                <h3 className="font-bold text-ink-800 mb-5">训练信息</h3>

                <div className="space-y-4">
                  <InfoRow
                    icon={
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <circle cx="12" cy="12" r="10" />
                        <polyline points="12 6 12 12 16 14" />
                      </svg>
                    }
                    label="预计时长"
                    value={duration}
                    color="text-brand-500"
                  />
                  <InfoRow
                    icon={
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                        <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                        <line x1="12" x2="12" y1="19" y2="22" />
                      </svg>
                    }
                    label="难度等级"
                    value={DIFFICULTY_LABELS[difficulty]}
                    color={difficulty === "easy" ? "text-ok" : difficulty === "medium" ? "text-warn" : "text-danger"}
                  />
                  <InfoRow
                    icon={
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <rect width="18" height="18" x="3" y="3" rx="2" />
                        <path d="M3 9h18" />
                        <path d="M9 21V9" />
                      </svg>
                    }
                    label="服务数量"
                    value={`${services.length} 个微服务`}
                    color="text-ai"
                  />
                  <InfoRow
                    icon={
                      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                        <polyline points="14 2 14 8 20 8" />
                        <path d="M9 13h6" />
                        <path d="M9 17h6" />
                      </svg>
                    }
                    label="业务领域"
                    value={DOMAIN_LABELS[project.domain]}
                    color="text-ink-600"
                  />
                </div>

                <div className="mt-6 pt-6 border-t border-ink-100">
                  <button
                    type="button"
                    onClick={handleStartTraining}
                    className="w-full h-11 rounded-xl bg-brand-500 hover:bg-brand-600 text-white font-semibold text-[14px] inline-flex items-center justify-center gap-2 transition-all shadow-md shadow-brand-500/20 hover:shadow-lg"
                  >
                    立即开始训练
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                      <polygon points="5 3 19 12 5 21 5 3" />
                    </svg>
                  </button>
                  <p className="mt-3 text-[11px] text-ink-400 text-center">
                    沙箱环境独立隔离，训练数据仅用于评估
                  </p>
                </div>
              </div>

              {/* AI 协作提示 */}
              <div className="bg-gradient-to-br from-ai/10 to-brand-50 rounded-2xl border border-ai/20 p-6">
                <div className="flex items-center gap-2 mb-3">
                  <span className="w-7 h-7 rounded-lg bg-white text-ai flex items-center justify-center shadow-sm">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3Z" />
                      <path d="M19 10v2a7 7 0 0 1-14 0v-2" />
                      <line x1="12" x2="12" y1="19" y2="22" />
                    </svg>
                  </span>
                  <span className="font-semibold text-ink-800 text-[14px]">AI 协作提示</span>
                </div>
                <p className="text-[13px] text-ink-600 leading-relaxed">
                  训练中 AI 助教会给出引导和建议，但<strong className="text-ai">部分建议可能是错误的</strong>。
                  请保持独立判断，通过实际观察和验证来确认问题根因。
                </p>
              </div>
            </div>
          </div>
        )}
      </main>

      {/* 风险告知弹窗 */}
      <RiskWarningModal
        open={riskModalOpen}
        onClose={() => setRiskModalOpen(false)}
        onConfirm={handleRiskConfirm}
        projectName={project?.name}
        confirming={riskConfirming}
        errorMessage={riskError}
      />

      {/* 沙箱启动进度（难度由系统判定，不再让用户选择） */}
      <SandboxLaunchModal
        open={launchModalOpen}
        progressPercent={sandboxPercent}
        progressStage={sandboxStage}
        errorMessage={startError}
        confirming={startConfirming}
        onClose={() => setLaunchModalOpen(false)}
        onRetry={startTraining}
      />
    </div>
  );
}

/* ===== 兜底数据 ===== */

/* ===== 子组件 ===== */

function InfoRow({
  icon,
  label,
  value,
  color,
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  color: string;
}) {
  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2.5 text-[13px] text-ink-500">
        <span className={color}>{icon}</span>
        {label}
      </div>
      <span className="text-[13px] font-semibold text-ink-700">{value}</span>
    </div>
  );
}
