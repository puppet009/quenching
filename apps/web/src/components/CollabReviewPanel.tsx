import { SkillRadar } from "./SkillRadar";
import { COLLAB_LEVELS, DIMENSION_LABELS } from "../services/radar";
import {
  type CollabReviewData,
  type TypicalQuestionItem,
} from "../services/review";

interface CollabReviewPanelProps {
  data: CollabReviewData;
  className?: string;
}

/**
 * 协作评估板块（T3-29）
 *
 * - 左侧：协作总分 + 等级 + 三因子分数条
 * - 右侧：协作维雷达图
 * - 底部：典型提问（好/坏对比）
 * - 纯人工模式（hasCollab=false）时不渲染
 */
export function CollabReviewPanel({ data, className = "" }: CollabReviewPanelProps) {
  if (!data.hasCollab) return null;

  const levelInfo = COLLAB_LEVELS[data.level];

  return (
    <div
      id="review-collab"
      className={`bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden ${className}`}
    >
      {/* 头部 */}
      <div className="px-6 pt-5 pb-4 border-b border-ink-100">
        <div className="flex items-center gap-3 mb-1">
          <span className="text-xl">🤝</span>
          <h3 className="text-base font-bold text-ink-900">AI 协作评估</h3>
        </div>
        <p className="text-[13px] text-ink-500 ml-9">
          从提问质量、判断力、验证习惯三个维度评估你的 AI 协作能力
        </p>
      </div>

      {/* 主体：三因子 + 雷达 */}
      <div className="px-6 py-5 grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* 左侧：分数 + 三因子 */}
        <div className="flex flex-col">
          {/* 总分 + 等级 */}
          <div className="flex items-center gap-4 mb-5">
            <div className="relative w-24 h-24 flex-shrink-0">
              <svg className="w-24 h-24 -rotate-90" viewBox="0 0 100 100">
                <circle
                  cx="50" cy="50" r="42"
                  fill="none" stroke="rgb(226, 232, 240)" strokeWidth="8"
                />
                <circle
                  cx="50" cy="50" r="42"
                  fill="none"
                  stroke="url(#collab-grad)"
                  strokeWidth="8"
                  strokeLinecap="round"
                  strokeDasharray={`${data.overall * 2.64} 264`}
                />
                <defs>
                  <linearGradient id="collab-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" stopColor="rgb(16, 185, 129)" />
                    <stop offset="100%" stopColor="rgb(139, 92, 246)" />
                  </linearGradient>
                </defs>
              </svg>
              <div className="absolute inset-0 flex flex-col items-center justify-center">
                <span className="text-2xl font-bold text-ink-800">{data.overall}</span>
                <span className="text-[10px] text-ink-400">协作分</span>
              </div>
            </div>
            <div>
              <div className={`text-lg font-bold ${levelInfo?.color ?? "text-ink-700"}`}>
                {levelInfo?.label ?? `L${data.level}`}
              </div>
              <div className="text-[12px] text-ink-500 mt-1 max-w-[180px]">
                {levelInfo?.desc ?? "继续提升你的 AI 协作能力"}
              </div>
            </div>
          </div>

          {/* 三因子进度条 */}
          <div className="space-y-3 flex-1">
            <FactorBar label="提问质量" value={data.factors.questionQuality} color="bg-ok-500" />
            <FactorBar label="判断力" value={data.factors.judgment} color="bg-brand-500" />
            <FactorBar label="验证习惯" value={data.factors.verificationHabit} color="bg-warn-500" />
          </div>
        </div>

        {/* 右侧：雷达图 */}
        <div className="flex flex-col items-center justify-center">
          <div className="text-[13px] font-semibold text-ink-700 mb-2">协作能力雷达</div>
          <SkillRadar
            data={{
              dimensions: data.radar.dimensions.map((d) => DIMENSION_LABELS[d] ?? d),
              values: data.radar.values,
            }}
            color="16, 185, 129"
            size={240}
            levels={4}
            labelSize={11}
          />
          <div className="text-[12px] text-ink-400 mt-2">6 维度综合评估</div>
        </div>
      </div>

      {/* 底部：典型提问 */}
      {data.typicalQuestions.length > 0 && (
        <div className="px-6 pb-5 border-t border-ink-50 pt-4">
          <div className="text-[13px] font-semibold text-ink-700 mb-3 flex items-center gap-2">
            <span>💬</span> 典型提问分析
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {data.typicalQuestions.map((q, i) => (
              <TypicalQuestionCard key={i} item={q} index={i + 1} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

/* ===== 子组件：因子进度条 ===== */

function FactorBar({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <span className="text-[12px] text-ink-600 font-medium">{label}</span>
        <span className="text-[12px] text-ink-700 font-semibold">{Math.round(value)}</span>
      </div>
      <div className="h-2 bg-ink-100 rounded-full overflow-hidden">
        <div
          className={`h-full ${color} rounded-full transition-all duration-700 ease-out`}
          style={{ width: `${Math.max(0, Math.min(100, value))}%` }}
        />
      </div>
    </div>
  );
}

/* ===== 子组件：典型提问卡片 ===== */

function TypicalQuestionCard({ item, index }: { item: TypicalQuestionItem; index: number }) {
  const isGood = item.type === "good";

  return (
    <div
      className={`p-3 rounded-xl border ${
        isGood
          ? "bg-ok-50 border-ok-200"
          : "bg-danger-50 border-danger-200"
      }`}
    >
      <div className="flex items-center gap-2 mb-2">
        <span className="text-lg">{isGood ? "👍" : "👎"}</span>
        <span
          className={`text-[11px] font-bold ${
            isGood ? "text-ok-700" : "text-danger-700"
          }`}
        >
          {isGood ? "高质量提问" : "需要改进"}
        </span>
        <span className="text-[10px] text-ink-400 ml-auto">#{index}</span>
      </div>
      <div
        className={`text-[13px] leading-relaxed mb-2 ${
          isGood ? "text-ok-800" : "text-danger-800"
        }`}
      >
        "{item.question}"
      </div>
      <div className="text-[12px] text-ink-600 bg-white/60 rounded-lg p-2">
        <span className="font-semibold">点评：</span>
        {item.comment}
      </div>
    </div>
  );
}
