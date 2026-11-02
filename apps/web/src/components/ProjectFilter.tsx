import { useEffect, useMemo, useState } from "react";
import { DOMAIN_TREE, domainLabel } from "@quenching/shared";

export type SortOption = "popular" | "newest";

interface ProjectFilterProps {
  /** 空值为全部，group:<code> 为大类，其余为细分领域。 */
  activeDomain: string;
  /** 全量模板的细分领域项目数 */
  counts: Record<string, number>;
  keyword: string;
  sortBy: SortOption;
  onDomainChange: (domain: string) => void;
  onKeywordChange: (keyword: string) => void;
  onSortChange: (sort: SortOption) => void;
  total: number;
}

const SORT_OPTIONS: { value: SortOption; label: string }[] = [
  { value: "popular", label: "默认排序" },
  { value: "newest", label: "最新上线" },
];

/**
 * 项目筛选工具栏：搜索、领域、排序及结果总数。
 * 固定领域树与快捷入口同步；树外的已上架领域保留回退选项。
 */
export function ProjectFilter({
  activeDomain,
  counts,
  keyword,
  sortBy,
  onDomainChange,
  onKeywordChange,
  onSortChange,
  total,
}: ProjectFilterProps) {
  const [searchInput, setSearchInput] = useState(keyword);
  const otherDomains = useMemo(() => {
    const knownCodes = new Set(DOMAIN_TREE.flatMap((group) => group.children.map((node) => node.code)));
    return Object.keys(counts).filter((code) => counts[code] > 0 && !knownCodes.has(code));
  }, [counts]);

  // 父级关键词变化时（如清除筛选）同步到输入框
  useEffect(() => {
    setSearchInput(keyword);
  }, [keyword]);

  // 实时搜索：输入即过滤（含中文输入法选字后的更新），无需按回车
  const handleInputChange = (value: string) => {
    setSearchInput(value);
    onKeywordChange(value.trim());
  };

  const handleClear = () => {
    setSearchInput("");
    onKeywordChange("");
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      <form role="search" onSubmit={(e) => e.preventDefault()} className="relative w-full shrink-0 sm:w-[280px]">
        <svg
          aria-hidden="true"
          className="absolute left-3 top-1/2 -translate-y-1/2 text-ink-400"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="11" cy="11" r="8" />
          <path d="m21 21-4.3-4.3" />
        </svg>
        <input
          type="text"
          value={searchInput}
          onChange={(e) => handleInputChange(e.target.value)}
          aria-label="搜索训练项目"
          placeholder="搜索项目名称、技术栈"
          className="h-9 w-full rounded-lg border border-ink-200 bg-white pl-9 pr-9 text-[13px] text-ink-700 placeholder:text-ink-400 focus:outline-none focus:border-brand-300 focus:ring-2 focus:ring-brand-500/10 transition-colors"
        />
        {searchInput && (
          <button
            type="button"
            onClick={handleClear}
            aria-label="清除搜索"
            className="absolute right-3 top-1/2 -translate-y-1/2 w-5 h-5 flex items-center justify-center rounded-full text-ink-400 hover:text-ink-600 hover:bg-ink-100 transition"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" />
              <path d="m6 6 12 12" />
            </svg>
          </button>
        )}
      </form>

      <select
        aria-label="按领域筛选"
        value={activeDomain}
        onChange={(e) => onDomainChange(e.target.value)}
        className="h-9 max-w-full w-[184px] cursor-pointer rounded-lg border border-ink-200 bg-white px-3 text-[13px] text-ink-700 focus:outline-none focus:border-brand-300 focus:ring-2 focus:ring-brand-500/10"
      >
        <option value="">全部领域</option>
        {DOMAIN_TREE.map((group) => (
          <optgroup key={group.code} label={group.label}>
            <option value={`group:${group.code}`}>{group.label} · 全部</option>
            {group.children.map((node) => (
              <option key={node.code} value={node.code} disabled={!counts[node.code]}>
                {node.label}（{counts[node.code] ? counts[node.code] : "敬请期待"}）
              </option>
            ))}
          </optgroup>
        ))}
        {otherDomains.length > 0 && (
          <optgroup label="其他领域">
            {otherDomains.map((code) => (
              <option key={code} value={code}>
                {domainLabel(code)}（{counts[code]}）
              </option>
            ))}
          </optgroup>
        )}
      </select>

      <select
        aria-label="项目排序"
        value={sortBy}
        onChange={(e) => onSortChange(e.target.value as SortOption)}
        className="h-9 max-w-full w-[116px] cursor-pointer rounded-lg border border-ink-200 bg-white px-3 text-[13px] text-ink-700 focus:outline-none focus:border-brand-300 focus:ring-2 focus:ring-brand-500/10"
      >
        {SORT_OPTIONS.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>

      <span className="whitespace-nowrap px-1 text-[12px] text-ink-400" aria-live="polite" aria-atomic="true">
        共 {total} 个项目
      </span>
    </div>
  );
}
