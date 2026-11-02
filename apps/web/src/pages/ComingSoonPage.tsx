import { Link } from "react-router-dom";

/**
 * 「即将上线」占位页
 *
 * 本期用于：挑战赛 /arena、论坛 /forum。实训环境入口已由 TrainingEntryPage 承接。
 * 标题与描述通过 props 配置，复用统一布局壳。
 */
export function ComingSoonPage({
  title,
  description,
}: {
  title: string;
  description?: string;
}) {
  return (
    <div className="page-container">
      <div className="min-h-[60vh] flex flex-col items-center justify-center text-center py-24">
        {/* 图标 */}
        <div className="w-16 h-16 rounded-2xl bg-brand-50 border border-brand-100 flex items-center justify-center mb-6">
          <svg
            width="30"
            height="30"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="text-brand-500"
          >
            <circle cx="12" cy="12" r="10" />
            <polyline points="12 6 12 12 16 14" />
          </svg>
        </div>

        <span className="inline-flex items-center px-2.5 py-1 rounded-full bg-ink-50 border border-ink-200 text-ink-500 text-[12px] font-medium mb-4">
          即将上线
        </span>
        <h1 className="text-2xl md:text-3xl font-bold text-ink-900 mb-3">{title}</h1>
        <p className="text-[15px] text-ink-500 leading-relaxed max-w-md">
          {description ?? "该功能正在紧锣密鼓地打磨中，敬请期待。"}
        </p>

        <div className="mt-9 flex items-center gap-3">
          <Link
            to="/projects"
            className="h-10 px-6 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-[14px] font-semibold inline-flex items-center transition-colors"
          >
            先去项目大厅
          </Link>
          <Link
            to="/"
            className="h-10 px-6 rounded-xl border border-ink-200 text-ink-600 hover:bg-white text-[14px] font-medium inline-flex items-center transition-colors"
          >
            返回首页
          </Link>
        </div>
      </div>
    </div>
  );
}
