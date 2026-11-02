import { useState, useEffect, useRef, useCallback, useMemo } from "react";
import { DISPLAY_TIME_ZONE, formatAbsDateTime, formatTimeZone } from "@quenching/shared";
import {
  LOG_LEVEL_CONFIG,
  formatLogTime,
  formatLogDate,
  type LogItem,
  type LogLevel,
} from "../services/logs";

interface LogViewerProps {
  /** 日志数据 */
  logs: LogItem[];
  /** 加载状态 */
  loading?: boolean;
  /** 是否自动滚动到底部（实时模式） */
  autoScroll?: boolean;
  /** 最大显示条数 */
  maxLines?: number;
  /** 是否显示工具栏 */
  showToolbar?: boolean;
  /** 服务列表（用于下拉选择） */
  services?: string[];
  /** 当前选中服务 */
  selectedService?: string;
  /** 服务切换回调 */
  onServiceChange?: (service: string) => void;
  /** 关键字搜索回调 */
  onSearch?: (keyword: string) => void;
  /** 级别过滤回调 */
  onLevelFilter?: (level: LogLevel | "all") => void;
  /** 实时刷新开关回调 */
  onLiveToggle?: (live: boolean) => void;
  /** 是否处于实时模式 */
  isLive?: boolean;
  /** 日志顺序：newest-first 新的在前（默认，兼容旧行为）；oldest-first 时间正序（实时 tail 追加） */
  order?: "newest-first" | "oldest-first";
  /** 是否还有更早的历史日志（游标翻页 has_more） */
  hasMore?: boolean;
  /** 历史日志加载中 */
  loadingMore?: boolean;
  /** 加载更早日志回调；传入后在列表边缘显示哨兵（IntersectionObserver 自动触发） */
  onLoadMore?: () => void;
  /** 导出回调；传入后工具栏显示导出按钮 */
  onExport?: () => void;
  /** 导出进行中 */
  exporting?: boolean;
  /** 导出命中后端上限被截断（X-Log-Export-Truncated=1） */
  exportTruncated?: boolean;
  /** 工具栏下方的非致命提示（如实时流数量上限 42901） */
  notice?: React.ReactNode;
  className?: string;
}

/**
 * 日志查看面板组件（T2-22）
 *
 * - 按服务/级别/关键字检索
 * - 实时刷新模式，自动滚动到底部
 * - 日志级别颜色区分
 * - 时间戳 + 级别 + 服务 + 内容 四列布局
 * - 单行可展开查看详情
 * - 加载骨架屏 / 空状态
 * - 关键词高亮
 */
export function LogViewer({
  logs,
  loading = false,
  autoScroll = true,
  maxLines = 200,
  showToolbar = true,
  services = [],
  selectedService = "",
  onServiceChange,
  onSearch,
  onLevelFilter,
  onLiveToggle,
  isLive = true,
  order = "newest-first",
  hasMore = false,
  loadingMore = false,
  onLoadMore,
  onExport,
  exporting = false,
  exportTruncated = false,
  notice,
  className = "",
}: LogViewerProps) {
  const [keyword, setKeyword] = useState("");
  const [levelFilter, setLevelFilter] = useState<LogLevel | "all">("all");
  const [expandedId, setExpandedId] = useState<number | null>(null);
  const [autoScrollEnabled, setAutoScrollEnabled] = useState(autoScroll);
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);

  const isOldestFirst = order === "oldest-first";

  // 过滤（按时间顺序展示，超出上限时：新在前裁尾部旧的，旧在前裁头部新的）
  const filteredLogs = useMemo(() => {
    const matched = logs.filter((log) => {
      if (levelFilter !== "all" && log.level !== levelFilter) return false;
      if (keyword && !log.message.toLowerCase().includes(keyword.toLowerCase())) return false;
      return true;
    });
    return isOldestFirst
      ? matched.slice(Math.max(0, matched.length - maxLines))
      : matched.slice(0, maxLines);
  }, [logs, levelFilter, keyword, maxLines, isOldestFirst]);

  // 自动滚动到底部（实时模式）
  useEffect(() => {
    if (!autoScrollEnabled || !scrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
  }, [logs, autoScrollEnabled]);

  // 游标翻页：哨兵进入可视区即自动加载更早/更多日志
  useEffect(() => {
    if (!onLoadMore || !hasMore || loadingMore) return;
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) onLoadMore();
      },
      { root: scrollRef.current, rootMargin: "120px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [onLoadMore, hasMore, loadingMore]);

  // 滚动时检测是否在底部，不在底部则暂停自动滚动
  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 30;
    if (isAtBottom !== autoScrollEnabled) {
      setAutoScrollEnabled(isAtBottom);
    }
  }, [autoScrollEnabled]);

  // 搜索提交
  const handleSearch = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();
      onSearch?.(keyword);
    },
    [keyword, onSearch],
  );

  // 级别过滤
  const handleLevelChange = useCallback(
    (level: LogLevel | "all") => {
      setLevelFilter(level);
      onLevelFilter?.(level);
    },
    [onLevelFilter],
  );

  // 滚动到底部
  const scrollToBottom = useCallback(() => {
    if (!scrollRef.current) return;
    scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    setAutoScrollEnabled(true);
  }, []);

  // 新日志数量提示
  const newLogsCount = logs.length > 0 ? Math.min(logs.length, 5) : 0;

  return (
    <div className={`bg-ink-900 rounded-xl overflow-hidden flex flex-col ${className}`}>
      {/* 工具栏 */}
      {showToolbar && (
        <div className="px-3 py-2 bg-ink-800 border-b border-ink-700 flex flex-wrap items-center gap-2">
          {/* 服务选择 */}
          {services.length > 0 && (
            <select
              value={selectedService}
              onChange={(e) => onServiceChange?.(e.target.value)}
              className="px-2 py-1 text-[12px] bg-ink-700 text-ink-200 border border-ink-600 rounded-md focus:outline-none focus:ring-1 focus:ring-brand-500"
            >
              <option value="">全部服务</option>
              {services.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          )}

          {/* 级别过滤 */}
          <div className="flex items-center gap-0.5 bg-ink-700 rounded-md p-0.5">
            {(["all", "error", "warn", "info", "debug"] as const).map((lvl) => (
              <button
                key={lvl}
                onClick={() => handleLevelChange(lvl)}
                className={`px-2 py-0.5 text-[11px] rounded font-medium transition-colors ${
                  levelFilter === lvl
                    ? "bg-brand-500 text-white"
                    : lvl === "all"
                      ? "text-ink-400 hover:text-ink-200"
                      : `${LOG_LEVEL_CONFIG[lvl as LogLevel].textColor} hover:bg-ink-600`
                }`}
              >
                {lvl === "all" ? "ALL" : LOG_LEVEL_CONFIG[lvl as LogLevel].label}
              </button>
            ))}
          </div>

          {/* 搜索框 */}
          <form onSubmit={handleSearch} className="flex-1 min-w-[120px]">
            <div className="relative">
              <input
                ref={searchInputRef}
                type="text"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                placeholder="搜索关键字..."
                className="w-full px-2.5 py-1 pr-7 text-[12px] bg-ink-700 text-ink-200 border border-ink-600 rounded-md placeholder-ink-500 focus:outline-none focus:ring-1 focus:ring-brand-500 focus:border-brand-500"
              />
              <span className="absolute right-2 top-1/2 -translate-y-1/2 text-ink-500 text-[11px]">
                🔍
              </span>
            </div>
          </form>

          {/* 实时开关 */}
          {onLiveToggle && (
            <button
              onClick={() => onLiveToggle(!isLive)}
              className={`px-2 py-1 text-[11px] font-medium rounded-md flex items-center gap-1 transition-colors ${
                isLive
                  ? "bg-ok-500/20 text-ok-400 border border-ok-500/30"
                  : "bg-ink-700 text-ink-400 border border-ink-600"
              }`}
            >
              <span
                className={`w-1.5 h-1.5 rounded-full ${
                  isLive ? "bg-ok-400 animate-pulse" : "bg-ink-500"
                }`}
              />
              {isLive ? "实时" : "暂停"}
            </button>
          )}

          {/* 导出日志 */}
          {onExport && (
            <button
              onClick={onExport}
              disabled={exporting}
              className="px-2 py-1 text-[11px] font-medium rounded-md bg-ink-700 text-ink-400 border border-ink-600 hover:bg-ink-600 hover:text-ink-200 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              title="导出当前筛选条件下的日志为文本文件"
            >
              {exporting ? "导出中…" : "⬇ 导出"}
            </button>
          )}

          {/* 滚动到底部按钮 */}
          <button
            onClick={scrollToBottom}
            className="px-2 py-1 text-[11px] font-medium rounded-md bg-ink-700 text-ink-400 border border-ink-600 hover:bg-ink-600 hover:text-ink-200 transition-colors"
            title="滚动到底部"
          >
            ⬇ 底部
          </button>
        </div>
      )}

      {/* 非致命提示条（42901 实时流上限 / 导出截断等） */}
      {(notice || exportTruncated) && (
        <div className="px-3 py-1.5 bg-warn-500/10 border-b border-warn-500/20 text-[11px] text-warn-200 flex items-start gap-1.5">
          <span className="flex-shrink-0">⚠</span>
          <span>
            {notice}
            {notice && exportTruncated && <br />}
            {exportTruncated &&
              "导出日志已达后端上限，结果已截断。请缩小时间范围或增加筛选条件后重试。"}
          </span>
        </div>
      )}

      {/* 统计栏 */}
      <div className="px-3 py-1 bg-ink-800/50 border-b border-ink-700/50 flex items-center justify-between text-[11px] text-ink-500">
        <span>
          显示 {filteredLogs.length} / {logs.length} 条
        </span>
        {logs.length > 0 && (
          <span>
            {formatLogDate(isOldestFirst ? logs[logs.length - 1].ts : logs[0].ts)}
          </span>
        )}
      </div>

      {/* 日志内容区 */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto font-mono text-[12px] leading-5"
        style={{ maxHeight: "400px", minHeight: "200px" }}
      >
        {loading ? (
          <SkeletonLogs count={8} />
        ) : filteredLogs.length === 0 ? (
          <EmptyState keyword={keyword} />
        ) : (
          <>
            {isOldestFirst && (
              <LoadMoreSentinel
                sentinelRef={sentinelRef}
                hasMore={hasMore}
                loadingMore={loadingMore}
                onLoadMore={onLoadMore}
                position="top"
              />
            )}
            <ul className="py-1">
              {filteredLogs.map((log, idx) => (
                <LogLine
                  key={`${log.ts}-${idx}`}
                  log={log}
                  index={idx}
                  keyword={keyword}
                  expanded={expandedId === idx}
                  onToggle={() => setExpandedId(expandedId === idx ? null : idx)}
                />
              ))}
            </ul>
            {!isOldestFirst && (
              <LoadMoreSentinel
                sentinelRef={sentinelRef}
                hasMore={hasMore}
                loadingMore={loadingMore}
                onLoadMore={onLoadMore}
                position="bottom"
              />
            )}
          </>
        )}
      </div>

      {/* 底部状态条 */}
      <div className="px-3 py-1.5 bg-ink-800 border-t border-ink-700 flex items-center justify-between text-[11px] text-ink-500">
        <div className="flex items-center gap-3">
          <span>显示时区: {DISPLAY_TIME_ZONE}</span>
          {isLive && (
            <span className="flex items-center gap-1 text-ok-400">
              <span className="w-1.5 h-1.5 rounded-full bg-ok-400 animate-pulse" />
              实时刷新中
            </span>
          )}
          {!autoScrollEnabled && filteredLogs.length > 0 && (
            <button
              onClick={scrollToBottom}
              className="text-brand-400 hover:text-brand-300 transition-colors"
            >
              {newLogsCount} 条新日志，点击查看
            </button>
          )}
        </div>
        <span>级别: {levelFilter === "all" ? "全部" : LOG_LEVEL_CONFIG[levelFilter].label}</span>
      </div>
    </div>
  );
}

/* ===== 子组件：游标加载哨兵 ===== */

function LoadMoreSentinel({
  sentinelRef,
  hasMore,
  loadingMore,
  onLoadMore,
  position,
}: {
  sentinelRef: React.RefObject<HTMLDivElement | null>;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore?: () => void;
  position: "top" | "bottom";
}) {
  if (!onLoadMore || (!hasMore && !loadingMore)) return null;
  return (
    <div ref={sentinelRef} className="px-3 py-2 text-center text-[11px] text-ink-500">
      {loadingMore
        ? "加载中…"
        : position === "top"
          ? "向上滚动自动加载更早日志"
          : "向下滚动自动加载更多日志"}
    </div>
  );
}

/* ===== 子组件：单条日志 ===== */

function LogLine({
  log,
  index,
  keyword,
  expanded,
  onToggle,
}: {
  log: LogItem;
  index: number;
  keyword: string;
  expanded: boolean;
  onToggle: () => void;
}) {
  const config = LOG_LEVEL_CONFIG[log.level];

  // 关键词高亮
  const highlightedMessage = keyword
    ? highlightKeyword(log.message, keyword)
    : log.message;

  return (
    <li
      onClick={onToggle}
      className={`
        px-3 py-0.5 cursor-pointer transition-colors
        [content-visibility:auto] [contain-intrinsic-size:auto_24px]
        ${index % 2 === 0 ? "bg-ink-900" : "bg-ink-900/50"}
        hover:bg-ink-800
      `}
    >
      <div className="flex items-start gap-2">
        {/* 时间戳 */}
        <span className="text-ink-500 flex-shrink-0 select-none" title={`${formatAbsDateTime(log.ts)} (${formatTimeZone(log.ts)})`}>
          {formatLogTime(log.ts)}
        </span>

        {/* 级别标签 */}
        <span
          className={`
            flex-shrink-0 w-[52px] text-center text-[10px] font-bold
            px-1 py-0.5 rounded
            ${config.bgColor} ${config.textColor}
          `}
        >
          {config.label}
        </span>

        {/* 服务名 */}
        <span className="text-brand-400 flex-shrink-0 max-w-[120px] truncate" title={log.service}>
          {log.service}
        </span>

        {/* 日志内容 */}
        <span className="text-ink-300 flex-1 break-all">
          {highlightedMessage}
        </span>
      </div>

      {/* 展开详情 */}
      {expanded && (
        <div className="mt-1 ml-2 pl-16 pb-1 border-l-2 border-ink-700 text-[11px] text-ink-500 space-y-0.5">
          {log.pod && <div>Pod: {log.pod}</div>}
          {log.namespace && <div>Namespace: {log.namespace}</div>}
          <div>时间（{formatTimeZone(log.ts)}）: {formatAbsDateTime(log.ts)}</div>
          <div>Level: {log.level.toUpperCase()}</div>
        </div>
      )}
    </li>
  );
}

/* ===== 子组件：关键词高亮 ===== */

function highlightKeyword(text: string, keyword: string): React.ReactNode {
  if (!keyword) return text;

  const lowerText = text.toLowerCase();
  const lowerKeyword = keyword.toLowerCase();
  const parts: React.ReactNode[] = [];
  let lastIndex = 0;
  let idx = lowerText.indexOf(lowerKeyword);

  while (idx !== -1) {
    if (idx > lastIndex) {
      parts.push(text.slice(lastIndex, idx));
    }
    parts.push(
      <mark
        key={`${idx}-${lastIndex}`}
        className="bg-warn-500/40 text-warn-200 px-0.5 rounded"
      >
        {text.slice(idx, idx + keyword.length)}
      </mark>,
    );
    lastIndex = idx + keyword.length;
    idx = lowerText.indexOf(lowerKeyword, lastIndex);
  }

  if (lastIndex < text.length) {
    parts.push(text.slice(lastIndex));
  }

  return <>{parts}</>;
}

/* ===== 子组件：骨架屏 ===== */

function SkeletonLogs({ count }: { count: number }) {
  return (
    <ul className="py-1 animate-pulse">
      {Array.from({ length: count }).map((_, i) => (
        <li
          key={i}
          className={`px-3 py-1 ${i % 2 === 0 ? "bg-ink-900" : "bg-ink-900/50"}`}
        >
          <div className="flex items-center gap-2">
            <div className="w-20 h-3 bg-ink-700 rounded" />
            <div className="w-10 h-3 bg-ink-700 rounded" />
            <div className="w-24 h-3 bg-ink-700 rounded" />
            <div className="flex-1 h-3 bg-ink-700 rounded" style={{ width: `${60 + Math.random() * 30}%` }} />
          </div>
        </li>
      ))}
    </ul>
  );
}

/* ===== 子组件：空状态 ===== */

function EmptyState({ keyword }: { keyword: string }) {
  return (
    <div className="py-12 text-center">
      <div className="text-3xl mb-2">{keyword ? "🔍" : "📋"}</div>
      <div className="text-[13px] text-ink-500 font-medium">
        {keyword ? "未找到匹配的日志" : "暂无日志数据"}
      </div>
      <div className="text-[11px] text-ink-600 mt-1">
        {keyword ? "请尝试其他关键字" : "等待日志输出..."}
      </div>
    </div>
  );
}
