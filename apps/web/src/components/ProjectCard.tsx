import {
  type Project,
  DOMAIN_LABELS,
  DOMAIN_ICONS,
  DIFFICULTY_LABELS,
  DIFFICULTY_COLORS,
  inferDifficulty,
  estimateDuration,
} from "@quenching/shared";

interface ProjectCardProps {
  project: Project;
  /** 是否已完成（MVP 阶段前端 mock，后续对接训练记录） */
  completed?: boolean;
  /** 学习人数（MVP 阶段模拟数据，后续对接后端） */
  learnerCount?: number;
  onClick?: (project: Project) => void;
}

/** 各领域封面渐变色（对应品牌色系统） */
const DOMAIN_GRADIENTS: Record<string, string> = {
  ecommerce: "from-brand-500 via-brand-400 to-ai",
  im: "from-ai via-brand-400 to-brand-500",
  saas: "from-ok via-brand-400 to-brand-500",
  iot: "from-warn via-brand-400 to-brand-500",
  game: "from-danger via-brand-400 to-brand-500",
};

/**
 * 项目卡片组件（打磨版）
 * - 渐变色封面 + 领域大图标
 * - 难度 + 领域双标签
 * - 技术栈标签
 * - 学习人数 / 时长 / 已完成标记
 * - hover 上浮 + 阴影加深
 */
export function ProjectCard({ project, completed = false, learnerCount, onClick }: ProjectCardProps) {
  const difficulty = inferDifficulty(project.tech_stack);
  const domainLabel = DOMAIN_LABELS[project.domain] ?? project.domain;
  const domainIcon = DOMAIN_ICONS[project.domain] ?? "📦";
  const duration = estimateDuration(difficulty);
  const gradient = DOMAIN_GRADIENTS[project.domain] ?? DOMAIN_GRADIENTS.ecommerce;
  const learners = learnerCount ?? Math.floor(Math.random() * 500) + 50; // MVP 模拟

  return (
    <div
      onClick={() => onClick?.(project)}
      className="group bg-white rounded-2xl border border-ink-100 overflow-hidden hover:border-brand-200 hover:shadow-xl hover:shadow-brand-500/5 transition-all duration-300 hover:-translate-y-1 cursor-pointer relative"
    >
      {/* 已完成角标 */}
      {completed && (
        <div className="absolute top-3 right-3 z-10 w-7 h-7 rounded-full bg-ok text-white flex items-center justify-center shadow-md">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
            <path d="M20 6 9 17l-5-5" />
          </svg>
        </div>
      )}

      {/* 卡片头部：渐变封面 */}
      <div className={`relative h-36 bg-gradient-to-br ${gradient} overflow-hidden`}>
        {/* 装饰光斑 */}
        <div className="absolute -top-8 -right-8 w-32 h-32 rounded-full bg-white/10 blur-xl" />
        <div className="absolute -bottom-10 -left-4 w-28 h-28 rounded-full bg-white/10 blur-xl" />

        {/* 领域大图标 */}
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-6xl opacity-30 group-hover:scale-110 group-hover:opacity-40 transition-all duration-500">
            {domainIcon}
          </span>
        </div>

        {/* 顶部标签 */}
        <div className="absolute top-4 left-4 flex items-center gap-2">
          <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full bg-white/20 backdrop-blur-sm text-[12px] font-medium text-white border border-white/20">
            <span>{domainIcon}</span>
            {domainLabel}
          </span>
        </div>
        <div className="absolute top-4 right-4">
          <span
            className={`inline-flex items-center px-2.5 py-1 rounded-full text-[12px] font-semibold backdrop-blur-sm border ${
              completed
                ? "bg-ok/90 text-white border-ok/30"
                : DIFFICULTY_COLORS[difficulty].replace("bg-", "bg-white/").replace("border-", "border-white/").replace("text-", "text-white")
            }`}
            style={!completed ? { backgroundColor: "rgba(255,255,255,0.2)", borderColor: "rgba(255,255,255,0.25)", color: "white" } : {}}
          >
            {completed ? "已完成" : DIFFICULTY_LABELS[difficulty]}
          </span>
        </div>
      </div>

      {/* 卡片内容 */}
      <div className="p-5">
        <h3 className="text-[15px] font-bold text-ink-800 mb-2 group-hover:text-brand-500 transition-colors line-clamp-1">
          {project.name}
        </h3>

        {/* 技术栈标签 */}
        <div className="flex flex-wrap gap-1.5 mb-4">
          {project.tech_stack?.slice(0, 3).map((tech) => (
            <span
              key={tech}
              className="px-2 py-0.5 rounded-md bg-ink-50 text-ink-500 text-[11px] font-medium border border-ink-100"
            >
              {tech}
            </span>
          ))}
          {project.tech_stack && project.tech_stack.length > 3 && (
            <span className="px-2 py-0.5 rounded-md bg-ink-50 text-ink-400 text-[11px] font-medium border border-ink-100">
              +{project.tech_stack.length - 3}
            </span>
          )}
        </div>

        {/* 底部信息栏 */}
        <div className="flex items-center justify-between pt-3 border-t border-ink-100">
          <div className="flex items-center gap-4 text-[12px] text-ink-400">
            <div className="flex items-center gap-1">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="10" />
                <polyline points="12 6 12 12 16 14" />
              </svg>
              {duration.replace("约 ", "")}
            </div>
            <div className="flex items-center gap-1">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
                <path d="M16 3.13a4 4 0 0 1 0 7.75" />
              </svg>
              {learners}人
            </div>
          </div>

          <div className="text-brand-500 text-[13px] font-semibold flex items-center gap-1 group-hover:gap-2 transition-all">
            开始
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14" />
              <path d="m12 5 7 7-7 7" />
            </svg>
          </div>
        </div>
      </div>
    </div>
  );
}
