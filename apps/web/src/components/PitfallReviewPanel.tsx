import { useState } from "react";
import {
  PITFALL_TYPE_LABELS,
  PITFALL_TYPE_COLORS,
  PITFALL_STATUS_LABELS,
  PITFALL_STATUS_COLORS,
  formatRelativeTime,
  type PitfallReviewData,
  type PitfallAnalysisItem,
} from "../services/review";

interface PitfallReviewPanelProps {
  data: PitfallReviewData;
  className?: string;
}

/**
 * 坑点回顾板块（T3-29）
 *
 * - 顶部统计卡片：总坑点数 / 识别数 / 踩坑数 / 识别率
 * - 时间线：每个坑点一条记录，可展开查看详情
 * - 每条包含：坑点类型、AI错误建议、你的反应、正确做法、改进建议
 * - 纯人工模式（hasAICollab=false）时不渲染
 */
export function PitfallReviewPanel({ data, className = "" }: PitfallReviewPanelProps) {
  // 纯人工模式隐藏
  if (!data.hasAICollab) return null;

  return (
    <div id="review-pitfall" className={`bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden ${className}`}>
      {/* 头部 */}
      <div className="px-6 pt-5 pb-4 border-b border-ink-100">
        <div className="flex items-center gap-3 mb-1">
          <span className="text-xl">🎭</span>
          <h3 className="text-base font-bold text-ink-900">AI 坑点回顾</h3>
        </div>
        <p className="text-[13px] text-ink-500 ml-9">
          本次训练共遇到 <span className="font-semibold text-ink-700">{data.total}</span> 个 AI 坑点，
          成功识别 <span className="font-semibold text-ok-600">{data.recognized}</span> 个
        </p>
      </div>

      {/* 统计条 */}
      <div className="px-6 py-4 grid grid-cols-4 gap-3 border-b border-ink-50">
        <StatCard label="总坑点数" value={data.total} color="text-ink-700" bg="bg-ink-50" />
        <StatCard
          label="识别成功"
          value={data.recognized}
          color="text-ok-600"
          bg="bg-ok-50"
        />
        <StatCard
          label="踩坑次数"
          value={data.fell}
          color="text-danger-600"
          bg="bg-danger-50"
        />
        <StatCard
          label="识别率"
          value={`${data.recognitionRate}%`}
          color="text-brand-600"
          bg="bg-brand-50"
        />
      </div>

      {/* 坑点时间线 */}
      <div className="px-6 py-4">
        <div className="space-y-3">
          {data.items.map((item, index) => (
            <PitfallTimelineItem key={item.pitfallId} item={item} index={index + 1} />
          ))}
        </div>

        {data.items.length === 0 && (
          <div className="py-10 text-center text-ink-400 text-[13px]">
            本次训练没有检测到坑点
          </div>
        )}
      </div>
    </div>
  );
}

/* ===== 子组件：统计卡片 ===== */

function StatCard({
  label,
  value,
  color,
  bg,
}: {
  label: string;
  value: number | string;
  color: string;
  bg: string;
}) {
  return (
    <div className={`${bg} rounded-xl p-3 text-center`}>
      <div className="text-[11px] text-ink-500 mb-1">{label}</div>
      <div className={`text-xl font-bold ${color} leading-tight`}>{value}</div>
    </div>
  );
}

/* ===== 子组件：时间线单条坑点 ===== */

function PitfallTimelineItem({ item, index }: { item: PitfallAnalysisItem; index: number }) {
  const [expanded, setExpanded] = useState(false);
  const typeStyle = PITFALL_TYPE_COLORS[item.type];
  const statusStyle = PITFALL_STATUS_COLORS[item.status];

  return (
    <div
      className={`relative pl-7 pb-4 border-l-2 ${typeStyle.dot.replace("bg-", "border-")} last:pb-0`}
    >
      {/* 时间线节点 */}
      <div
        className={`absolute -left-[9px] top-0 w-4 h-4 rounded-full ${typeStyle.dot} border-2 border-white shadow-sm`}
      />

      {/* 卡片 */}
      <div
        className={`rounded-xl border ${typeStyle.border} ${typeStyle.bg} overflow-hidden transition-all duration-200`}
      >
        {/* 头部：类型 + 状态 + 时间 */}
        <div className="px-4 py-3 flex items-center justify-between cursor-pointer hover:bg-white/50"
             onClick={() => setExpanded(!expanded)}>
          <div className="flex items-center gap-2">
            <span className="text-[11px] text-ink-400 font-mono">#{index}</span>
            <span className={`text-[12px] font-semibold ${typeStyle.text}`}>
              {PITFALL_TYPE_LABELS[item.type]}
            </span>
            <ConfidenceBadge level={item.confidence} />
          </div>
          <div className="flex items-center gap-2">
            <span className={`text-[12px] font-medium px-2 py-0.5 rounded-full ${statusStyle.bg} ${statusStyle.text}`}>
              {statusStyle.icon} {PITFALL_STATUS_LABELS[item.status]}
            </span>
            <span className="text-[11px] text-ink-400 font-mono">
              {formatRelativeTime(item.appearedAtMs)}
            </span>
            <svg
              className={`w-4 h-4 text-ink-400 transition-transform duration-200 ${expanded ? "rotate-180" : ""}`}
              fill="none" viewBox="0 0 24 24" stroke="currentColor"
            >
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </div>

        {/* 坑点名称 */}
        <div className="px-4 pb-2">
          <div className="text-[14px] font-semibold text-ink-800">{item.name}</div>
        </div>

        {/* 展开详情 */}
        {expanded && (
          <div className="px-4 pb-4 space-y-3 border-t border-white/60 pt-3">
            {/* AI 说了什么 */}
            <DetailBlock
              icon="🤖"
              title="AI 的建议"
              content={item.aiSuggestion}
              accent="danger"
            />

            {/* 为什么容易被误导 */}
            <DetailBlock
              icon="💭"
              title="为什么容易被误导"
              content={item.whyMisleading}
              accent="warn"
            />

            {/* 你的反应 */}
            <DetailBlock
              icon="👤"
              title={`你的反应 · ${PITFALL_STATUS_LABELS[item.status]}`}
              content={item.userAction}
              accent={item.status === "recognized" ? "ok" : item.status === "fell" ? "danger" : "ink"}
            />

            {/* 正确做法 */}
            <DetailBlock
              icon="✅"
              title="正确的思路"
              content={item.correctApproach}
              accent="ok"
            />

            {/* 改进建议 */}
            <div className="p-3 bg-brand-50 rounded-lg border border-brand-100">
              <div className="text-[12px] font-semibold text-brand-700 mb-1 flex items-center gap-1">
                <span>💡</span> 改进建议
              </div>
              <div className="text-[13px] text-brand-800 leading-relaxed">
                {item.improvementTip}
              </div>
            </div>

            {/* 恶化提示 */}
            {item.worsened && (
              <div className="p-3 bg-danger-50 rounded-lg border border-danger-200">
                <div className="text-[12px] font-semibold text-danger-700 flex items-center gap-1">
                  <span>⚠️</span> 此坑导致问题恶化
                </div>
                <div className="text-[12px] text-danger-600 mt-1">
                  采纳错误建议后，故障影响范围扩大，排查时间增加
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/* ===== 子组件：详情块 ===== */

function DetailBlock({
  icon,
  title,
  content,
  accent,
}: {
  icon: string;
  title: string;
  content: string;
  accent: "ok" | "danger" | "warn" | "ink";
}) {
  const colorMap = {
    ok: "text-ok-700 border-ok-200 bg-ok-50",
    danger: "text-danger-700 border-danger-200 bg-danger-50",
    warn: "text-warn-700 border-warn-200 bg-warn-50",
    ink: "text-ink-700 border-ink-200 bg-ink-50",
  };

  return (
    <div className={`p-3 rounded-lg border ${colorMap[accent]}`}>
      <div className="text-[12px] font-semibold mb-1 flex items-center gap-1">
        <span>{icon}</span> {title}
      </div>
      <div className="text-[13px] leading-relaxed opacity-90">{content}</div>
    </div>
  );
}

/* ===== 子组件：置信度徽章 ===== */

function ConfidenceBadge({ level }: { level: "low" | "medium" | "high" }) {
  const config = {
    low: { label: "低置信", cls: "bg-ink-100 text-ink-600" },
    medium: { label: "中置信", cls: "bg-warn-100 text-warn-700" },
    high: { label: "高置信", cls: "bg-brand-100 text-brand-700" },
  }[level];

  return (
    <span className={`text-[10px] px-1.5 py-0.5 rounded ${config.cls} font-medium`}>
      {config.label}
    </span>
  );
}
