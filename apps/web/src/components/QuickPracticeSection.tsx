import { DOMAIN_TREE, domainGroupOf } from "@quenching/shared";

interface QuickPracticeSectionProps {
  /** 全量模板的细分领域项目数，缺省为 0。 */
  counts: Record<string, number>;
  /** 空值为全部，group:<code> 为大类，其余为细分领域。 */
  activeDomain: string;
  visibleGroupCode: string;
  onDomainChange: (domain: string) => void;
}

/** 固定领域地图：五个大类始终展示，当前大类的全部细分保留待上线入口。 */
export function QuickPracticeSection({
  counts,
  activeDomain,
  visibleGroupCode,
  onDomainChange,
}: QuickPracticeSectionProps) {
  const visibleGroup = DOMAIN_TREE.find((group) => group.code === visibleGroupCode) ?? DOMAIN_TREE[0];
  const activeGroupCode = activeDomain.startsWith("group:")
    ? activeDomain.slice("group:".length)
    : domainGroupOf(activeDomain)?.code;

  return (
    <section aria-label="按领域练习" className="mb-3 space-y-2">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
        {DOMAIN_TREE.map((group) => {
          const total = group.children.reduce((sum, node) => sum + (counts[node.code] ?? 0), 0);
          const active = activeGroupCode === group.code;
          return (
            <button
              key={group.code}
              type="button"
              aria-label={`筛选${group.label}`}
              aria-pressed={active}
              aria-expanded={visibleGroup.code === group.code}
              aria-controls="domain-subtypes"
              onClick={() => onDomainChange(`group:${group.code}`)}
              className={`flex h-11 min-w-0 items-center gap-2 rounded-lg border px-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${
                active
                  ? "border-brand-300 bg-brand-50/60"
                  : "border-ink-100 bg-white hover:border-brand-200 hover:bg-brand-50/30"
              }`}
            >
              <span
                aria-hidden="true"
                className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-gradient-to-br ${group.gradient} text-[12px] font-bold text-white`}
              >
                {group.label.charAt(0)}
              </span>
              <span className="min-w-0">
                <span className="block truncate text-[13px] font-semibold leading-4 text-ink-800">
                  {group.label}
                </span>
                <span className="block text-[10px] leading-3 text-ink-500">
                  {group.children.length} 细分 · {total} 项目
                </span>
              </span>
            </button>
          );
        })}
      </div>

      <div
        id="domain-subtypes"
        role="group"
        aria-label={`${visibleGroup.label}细分领域`}
        className="flex flex-wrap items-center gap-1.5 rounded-lg border border-ink-100 bg-white p-2"
      >
        <button
          type="button"
          aria-pressed={!activeDomain}
          onClick={() => onDomainChange("")}
          className={`inline-flex h-7 shrink-0 items-center rounded-md border px-2 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${
            !activeDomain
              ? "border-brand-200 bg-brand-50 text-brand-600"
              : "border-transparent text-ink-500 hover:bg-ink-50 hover:text-brand-600"
          }`}
        >
          全部领域
        </button>
        {visibleGroup.children.map((node) => {
          const count = counts[node.code] ?? 0;
          const available = count > 0;
          const active = activeDomain === node.code;
          return (
            <button
              key={node.code}
              type="button"
              disabled={!available}
              aria-pressed={active}
              onClick={() => onDomainChange(node.code)}
              className={`inline-flex h-7 shrink-0 items-center gap-1 rounded-md border px-2 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 ${
                active
                  ? "border-brand-500 bg-brand-500 text-white"
                  : available
                    ? "border-ink-100 bg-ink-50 text-ink-600 hover:border-brand-300 hover:text-brand-600"
                    : "cursor-not-allowed border-dashed border-ink-200 bg-ink-50/50 text-ink-400"
              }`}
            >
              {node.label}
              <span className={`text-[10px] ${active ? "text-white/80" : available ? "text-brand-500" : "text-ink-400"}`}>
                {available ? count : "敬请期待"}
              </span>
            </button>
          );
        })}
      </div>
    </section>
  );
}
