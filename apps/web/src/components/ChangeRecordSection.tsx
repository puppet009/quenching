// 助手修改记录章节（M5）
//
// 复盘页中 section_type === "assistant_changes" 的章节专用渲染：
// 优先用 content_data 中的结构化数据渲染「文件修改卡片」
// （新建/覆盖标记、发生时间、Pod/容器、路径、写入前/写入后内容）；
// content_data 缺失或解析失败时由调用方退回 Markdown 正文渲染。
//
// 这些改动只发生在本次实训沙箱内，沙箱销毁后随之清除，此处仅作操作留痕。

import { useMemo, useState } from "react";
import {
  parseSectionData,
  formatOffsetSec,
  type ChangeAnalysisData,
  type ChangeItemData,
  type ReviewSection,
} from "../services/review";

/** 正文代码块最大展示行数，超出部分折叠（保留全文，可展开查看） */
const CODE_COLLAPSE_LINES = 40;

/**
 * 解析章节结构化载荷。返回 null 表示无法结构化渲染，
 * 调用方应退回 Markdown。
 */
export function useChangeAnalysis(section: ReviewSection): ChangeAnalysisData | null {
  return useMemo(() => {
    const data = parseSectionData<ChangeAnalysisData>(section.content_data);
    if (!data || !Array.isArray(data.changes) || data.changes.length === 0) {
      return null;
    }
    return data;
  }, [section.content_data]);
}

export function ChangeRecordSection({ section }: { section: ReviewSection }) {
  const analysis = useChangeAnalysis(section);
  if (!analysis) return null;

  const newCount = analysis.changes.filter((c) => !c.before_existed).length;
  const overwriteCount = analysis.total_count - newCount;

  return (
    <div className="space-y-4">
      {/* 概述统计 */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-ink-100 text-[12px] text-ink-600">
          <span className="font-semibold text-ink-800">{analysis.total_count}</span> 次修改
        </span>
        <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-ok-50 text-[12px] text-ok-700 border border-ok-100">
          新建 <span className="font-semibold">{newCount}</span>
        </span>
        <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-warn-50 text-[12px] text-warn-700 border border-warn-100">
          覆盖 <span className="font-semibold">{overwriteCount}</span>
        </span>
        <span className="text-[11px] text-ink-400">
          改动仅存在于本次沙箱，沙箱销毁后随之清除，此处仅作操作留痕
        </span>
      </div>

      {/* 逐条修改卡片 */}
      <div className="space-y-3">
        {analysis.changes.map((item, idx) => (
          <ChangeCard key={item.id ?? idx} index={idx + 1} item={item} />
        ))}
      </div>
    </div>
  );
}

/* ===================== 单条修改卡片 ===================== */

function ChangeCard({ index, item }: { index: number; item: ChangeItemData }) {
  const [open, setOpen] = useState(false);
  const isNew = !item.before_existed;

  return (
    <div
      className={`rounded-xl border overflow-hidden ${
        isNew ? "border-ok-100 bg-ok-50/30" : "border-warn-100 bg-warn-50/30"
      }`}
    >
      {/* 卡片头 */}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-3 px-4 py-3 text-left hover:bg-white/60 transition-colors"
      >
        <span
          className={`flex-shrink-0 inline-flex items-center px-2 py-0.5 rounded-md text-[11px] font-medium ${
            isNew
              ? "bg-ok-100 text-ok-700"
              : "bg-warn-100 text-warn-700"
          }`}
        >
          {isNew ? "新建" : "覆盖"}
        </span>
        <span className="flex-shrink-0 text-[12px] text-ink-500">{formatOffsetSec(item.t)}</span>
        <span className="flex-1 min-w-0 font-mono text-[12.5px] text-ink-800 truncate">
          {item.path}
        </span>
        <span className="flex-shrink-0 text-[11px] text-ink-400 hidden sm:inline">
          {item.pod}
          {item.container ? ` / ${item.container}` : ""}
        </span>
        <span className="flex-shrink-0 text-ink-400 text-[12px] w-4 text-center">
          {open ? "∧" : "∨"}
        </span>
      </button>

      {/* 卡片内容：写入前 / 写入后 */}
      {open && (
        <div className="px-4 pb-4 space-y-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-ink-400">
            <span>
              实例：<span className="font-mono">{item.pod || "—"}</span>
            </span>
            {item.container && (
              <span>
                容器：<span className="font-mono">{item.container}</span>
              </span>
            )}
            <span className="text-ink-300">#{index}</span>
          </div>

          {!isNew && (
            <CodePanel
              title="写入前"
              tone="old"
              content={item.before_content ?? ""}
            />
          )}
          <CodePanel title="写入后" tone="new" content={item.after_content ?? ""} />
        </div>
      )}
    </div>
  );
}

/* ===================== 代码面板 ===================== */

function CodePanel({
  title,
  tone,
  content,
}: {
  title: string;
  tone: "old" | "new";
  content: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const lines = content.split("\n");
  const overflow = lines.length > CODE_COLLAPSE_LINES;
  const shown = expanded || !overflow ? content : lines.slice(0, CODE_COLLAPSE_LINES).join("\n");
  const empty = content.length === 0;

  const badge =
    tone === "old"
      ? "bg-ink-200/70 text-ink-600"
      : "bg-brand-100 text-brand-700";

  return (
    <div>
      <div className="flex items-center gap-2 mb-1.5">
        <span className={`text-[11px] px-2 py-0.5 rounded ${badge}`}>{title}</span>
        <span className="text-[11px] text-ink-400">
          {empty ? "空内容" : `${lines.length} 行`}
        </span>
      </div>
      {empty ? (
        <p className="text-[12px] text-ink-400 px-3 py-2 bg-ink-900 rounded-lg text-ink-100">
          （文件为空）
        </p>
      ) : (
        <>
          <pre className="m-0 p-3 bg-ink-900 text-ink-100 rounded-lg overflow-x-auto text-[12px] leading-relaxed font-mono whitespace-pre">
            {shown}
            {overflow && !expanded ? "\n…" : ""}
          </pre>
          {overflow && (
            <button
              type="button"
              onClick={() => setExpanded((v) => !v)}
              className="mt-1.5 text-[12px] text-brand-600 hover:text-brand-700"
            >
              {expanded ? "收起" : `展开全部 ${lines.length} 行`}
            </button>
          )}
        </>
      )}
    </div>
  );
}
