interface PaginationProps {
  currentPage: number;
  totalPages: number;
  totalItems: number;
  pageSize: number;
  onPageChange: (page: number) => void;
}

/**
 * 分页器组件
 * - 上一页/下一页按钮
 * - 页码按钮（中间省略号）
 * - 总条目数显示
 */
export function Pagination({
  currentPage,
  totalPages,
  totalItems,
  pageSize,
  onPageChange,
}: PaginationProps) {
  if (totalPages <= 1) return null;

  const startItem = (currentPage - 1) * pageSize + 1;
  const endItem = Math.min(currentPage * pageSize, totalItems);

  // 生成页码数组（含省略号）
  const getPageNumbers = (): (number | "...")[] => {
    const pages: (number | "...")[] = [];
    const delta = 1; // 当前页左右各显示几个

    // 始终显示第一页
    pages.push(1);

    // 左边省略号
    if (currentPage - delta > 2) {
      pages.push("...");
    }

    // 中间页码
    const left = Math.max(2, currentPage - delta);
    const right = Math.min(totalPages - 1, currentPage + delta);
    for (let i = left; i <= right; i++) {
      pages.push(i);
    }

    // 右边省略号
    if (currentPage + delta < totalPages - 1) {
      pages.push("...");
    }

    // 始终显示最后一页（如果大于 1）
    if (totalPages > 1) {
      pages.push(totalPages);
    }

    return pages;
  };

  const pageNumbers = getPageNumbers();

  return (
    <div className="flex flex-col sm:flex-row items-center justify-between gap-4 mt-8">
      {/* 条目信息 */}
      <div className="text-[13px] text-ink-500">
        显示第 <span className="font-semibold text-ink-700">{startItem}</span> -{" "}
        <span className="font-semibold text-ink-700">{endItem}</span> 条，共{" "}
        <span className="font-semibold text-ink-700">{totalItems}</span> 个项目
      </div>

      {/* 分页按钮 */}
      <div className="flex items-center gap-1">
        {/* 上一页 */}
        <button
          type="button"
          onClick={() => onPageChange(currentPage - 1)}
          disabled={currentPage === 1}
          className="h-9 w-9 rounded-lg flex items-center justify-center text-ink-500 hover:bg-ink-100 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          aria-label="上一页"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m15 18-6-6 6-6" />
          </svg>
        </button>

        {/* 页码 */}
        {pageNumbers.map((page, idx) =>
          page === "..." ? (
            <span
              key={`ellipsis-${idx}`}
              className="h-9 w-9 flex items-center justify-center text-ink-400 text-[13px]"
            >
              ...
            </span>
          ) : (
            <button
              key={page}
              type="button"
              onClick={() => onPageChange(page)}
              className={`h-9 min-w-[36px] px-3 rounded-lg text-[13px] font-medium transition-all ${
                currentPage === page
                  ? "bg-brand-500 text-white shadow-md shadow-brand-500/20"
                  : "text-ink-600 hover:bg-ink-100"
              }`}
            >
              {page}
            </button>
          ),
        )}

        {/* 下一页 */}
        <button
          type="button"
          onClick={() => onPageChange(currentPage + 1)}
          disabled={currentPage === totalPages}
          className="h-9 w-9 rounded-lg flex items-center justify-center text-ink-500 hover:bg-ink-100 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          aria-label="下一页"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="m9 18 6-6-6-6" />
          </svg>
        </button>
      </div>
    </div>
  );
}
