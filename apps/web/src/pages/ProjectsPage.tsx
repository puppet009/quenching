import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ProjectFilter, type SortOption } from "../components/ProjectFilter";
import { QuickPracticeSection } from "../components/QuickPracticeSection";
import { TemplateCover } from "../components/TemplateCover";
import {
  listTemplates,
  domainLabel,
  domainGroupOf,
  DOMAIN_TREE,
  type Template,
} from "@quenching/shared";

/**
 * 项目大厅页（模板文件注册表版）
 * - 数据来自 /templates（templates/<slug>/template.yaml 扫描结果），全量加载
 * - 固定五大类及当前大类的全部细分紧凑展示，未上架领域保留「敬请期待」
 * - 大类、细分与工具栏共用筛选状态，项目数由全量模板 domain 实时统计
 * - 我的训练入口由 /history 路由统一鉴权
 */
export function ProjectsPage() {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>("");

  const [activeDomain, setActiveDomain] = useState<string>("");
  const [visibleGroupCode, setVisibleGroupCode] = useState(DOMAIN_TREE[0].code);
  const [keyword, setKeyword] = useState<string>("");
  const [sortBy, setSortBy] = useState<SortOption>("popular");

  useEffect(() => {
    setLoading(true);
    setError("");
    listTemplates()
      .then((list) => setTemplates(list ?? []))
      .catch((e) => {
        setError(e?.message ?? "加载失败，请稍后重试");
        setTemplates([]);
      })
      .finally(() => setLoading(false));
  }, []);

  // 细分领域 code → 项目数
  const counts = useMemo(() => {
    const m: Record<string, number> = {};
    for (const t of templates) {
      if (t.domain) m[t.domain] = (m[t.domain] ?? 0) + 1;
    }
    return m;
  }, [templates]);

  const activeGroup = activeDomain.startsWith("group:")
    ? DOMAIN_TREE.find((group) => group.code === activeDomain.slice("group:".length))
    : undefined;
  const groupAwaitingProjects = activeGroup && activeGroup.children.every((node) => !counts[node.code]);

  const handleDomainChange = (domain: string) => {
    setActiveDomain(domain);
    const group = domain.startsWith("group:")
      ? DOMAIN_TREE.find((item) => item.code === domain.slice("group:".length))
      : domainGroupOf(domain);
    if (group) setVisibleGroupCode(group.code);
  };

  // 过滤 + 排序（全量前端处理）
  const filtered = useMemo(() => {
    let result = [...templates];
    if (activeGroup) {
      result = result.filter((t) => activeGroup.children.some((node) => node.code === t.domain));
    } else if (activeDomain) {
      result = result.filter((t) => t.domain === activeDomain);
    }
    if (keyword) {
      const kw = keyword.toLowerCase();
      result = result.filter(
        (t) =>
          t.name.toLowerCase().includes(kw) ||
          (t.description ?? "").toLowerCase().includes(kw) ||
          t.slug.toLowerCase().includes(kw) ||
          (t.tech_stack ?? []).some((s) => s.toLowerCase().includes(kw)),
      );
    }
    if (sortBy === "newest") {
      result.sort((a, b) => b.sort_order - a.sort_order);
    } else {
      result.sort((a, b) => b.sort_order - a.sort_order || a.slug.localeCompare(b.slug));
    }
    return result;
  }, [templates, activeDomain, activeGroup, keyword, sortBy]);

  return (
    <div>
      <main className="page-container py-6">
        {/* 页面标题 */}
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h1 className="text-2xl font-bold text-ink-900">发现训练项目</h1>
            <p className="mt-1 text-[13px] text-ink-500">
              选择适合你的训练项目，在真实沙箱中淬炼排障能力与 AI 协作判断力
            </p>
          </div>
          <Link
            to="/history"
            className="inline-flex h-9 shrink-0 items-center gap-1 rounded-lg px-3 text-[13px] font-medium text-ink-600 transition-colors hover:bg-white hover:text-brand-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
          >
            我的训练
            <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="m9 5 7 7-7 7" />
            </svg>
          </Link>
        </div>

        <QuickPracticeSection
          counts={counts}
          activeDomain={activeDomain}
          visibleGroupCode={visibleGroupCode}
          onDomainChange={handleDomainChange}
        />

        {/* 筛选栏 */}
        <div id="project-list">
          <ProjectFilter
            activeDomain={activeDomain}
            counts={counts}
            keyword={keyword}
            sortBy={sortBy}
            onDomainChange={handleDomainChange}
            onKeywordChange={setKeyword}
            onSortChange={setSortBy}
            total={filtered.length}
          />
        </div>

        {/* 列表区 */}
        <div className="mt-5">
          {/* 加载中 */}
          {loading && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {Array.from({ length: 3 }).map((_, i) => (
                <div
                  key={i}
                  className="bg-white rounded-2xl border border-ink-100 overflow-hidden animate-pulse"
                >
                  <div className="h-36 bg-gradient-to-br from-ink-100 to-ink-50" />
                  <div className="p-5 space-y-3">
                    <div className="h-5 bg-ink-100 rounded w-3/4" />
                    <div className="h-4 bg-ink-100 rounded w-1/2" />
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* 错误态 */}
          {!loading && error && (
            <div className="text-center py-20">
              <div className="w-20 h-20 mx-auto mb-5 rounded-full bg-danger/10 flex items-center justify-center">
                <svg width="36" height="36" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-danger">
                  <circle cx="12" cy="12" r="10" />
                  <line x1="12" y1="8" x2="12" y2="12" />
                  <line x1="12" y1="16" x2="12.01" y2="16" />
                </svg>
              </div>
              <h3 className="text-lg font-bold text-ink-700 mb-2">加载失败</h3>
              <p className="text-ink-500 text-[14px] mb-6">{error}</p>
              <button
                type="button"
                onClick={() => window.location.reload()}
                className="h-10 px-6 rounded-lg bg-brand-500 hover:bg-brand-600 text-white font-medium text-[14px] transition-colors"
              >
                重新加载
              </button>
            </div>
          )}

          {/* 空状态 */}
          {!loading && !error && filtered.length === 0 && (
            <div className="text-center py-20 bg-white rounded-2xl border border-ink-100">
              <h3 className="text-lg font-bold text-ink-700 mb-2">
                {groupAwaitingProjects ? `${activeGroup.label}项目待上线` : "没有找到匹配的项目"}
              </h3>
              <p className="text-ink-500 text-[14px]">
                {groupAwaitingProjects
                  ? "该领域正在建设中，敬请期待；可选择全部领域查看已上线项目"
                  : keyword
                  ? `没有与“${keyword}”相关的项目，换个关键词试试`
                  : "暂无可用的训练项目，请稍后再来"}
              </p>
            </div>
          )}

          {/* 模板卡片网格 */}
          {!loading && !error && filtered.length > 0 && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {filtered.map((t) => (
                <TemplateCard key={t.slug} template={t} />
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

/** 模板卡片，优先展示模板自带的单张封面。 */
function TemplateCard({ template }: { template: Template }) {
  const stack = (template.tech_stack ?? []).slice(0, 3);

  return (
    <Link
      to={`/projects/t/${template.slug}`}
      className="group block bg-white rounded-2xl border border-ink-100 overflow-hidden hover:border-brand-200 hover:shadow-xl hover:shadow-brand-500/5 transition-all duration-300 hover:-translate-y-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-2"
    >
      <div className="relative h-44 overflow-hidden">
        <TemplateCover template={template} compact className="h-full" />
        <span className="absolute top-4 left-4 px-2.5 py-1 rounded-full bg-white/90 backdrop-blur-sm text-[12px] font-medium text-ink-700 border border-white/80">
          {domainLabel(template.domain)}
        </span>
      </div>

      <div className="p-5">
        <h3 className="text-[15px] font-bold text-ink-800 mb-2 group-hover:text-brand-500 transition-colors line-clamp-1">
          {template.name}
        </h3>
        <p className="text-[12px] text-ink-400 mb-3 line-clamp-2 min-h-[32px]">
          {template.description}
        </p>
        {stack.length > 0 && (
          <div className="flex flex-wrap gap-1.5 mb-4">
            {stack.map((s) => (
              <span key={s} className="px-2 py-0.5 rounded-md bg-ink-50 text-ink-500 text-[11px] font-medium border border-ink-100">
                {s}
              </span>
            ))}
          </div>
        )}
        <div className="flex items-center justify-end pt-3 border-t border-ink-100">
          <span className="text-brand-500 text-[13px] font-semibold flex items-center gap-1 group-hover:gap-2 transition-all">
            查看
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14" />
              <path d="m12 5 7 7-7 7" />
            </svg>
          </span>
        </div>
      </div>
    </Link>
  );
}
