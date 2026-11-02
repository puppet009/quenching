// NPS 评价收集组件（T5-9）
//
// 轻量不打断的评价收集：
//   1. 快速评价（有价值 / 没价值）— 一键点击
//   2. NPS 评分（0-10） — 滑动/点击选择
//   3. 开放反馈（选填）
//
// 设计原则：
//   - 先展示简单的二选一，降低评价门槛
//   - 用户选择后展开 NPS 评分
//   - 评分后展开文本反馈
//   - 全程不弹 Modal，嵌在页面底部

import { useState } from "react";
import { NPS_SCORE_LABELS, getNpsCategory } from "@quenching/shared";

interface NpsSurveyProps {
  trainingId?: number;
  onSubmit?: (data: {
    score: number;
    value_feedback?: "valuable" | "not_valuable";
    comment?: string;
  }) => void;
  className?: string;
}

const NPS_CAT_CONFIG = {
  promoter: { label: "推荐者", color: "text-ok-600", bg: "bg-ok-50" },
  passive: { label: "中立者", color: "text-warn-600", bg: "bg-warn-50" },
  detractor: { label: "贬损者", color: "text-danger-600", bg: "bg-danger-50" },
};

/**
 * NPS 评价收集组件（轻量嵌入版）
 *
 * 三步渐进式：快速评价 → NPS 评分 → 开放反馈
 */
export function NpsSurvey({ trainingId, onSubmit, className = "" }: NpsSurveyProps) {
  // trainingId 预留，用于提交时关联训练记录
  void trainingId;
  const [valueFeedback, setValueFeedback] = useState<"valuable" | "not_valuable" | null>(null);
  const [score, setScore] = useState<number | null>(null);
  const [hoveredScore, setHoveredScore] = useState<number | null>(null);
  const [comment, setComment] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleValueClick = (value: "valuable" | "not_valuable") => {
    if (submitted) return;
    setValueFeedback(value);
  };

  const handleScoreClick = (s: number) => {
    if (submitted) return;
    setScore(s);
  };

  const handleSubmit = async () => {
    if (score === null || submitting) return;

    setSubmitting(true);
    try {
      await new Promise((r) => setTimeout(r, 500)); // 模拟请求
      onSubmit?.({
        score,
        value_feedback: valueFeedback || undefined,
        comment: comment.trim() || undefined,
      });
      setSubmitted(true);
    } finally {
      setSubmitting(false);
    }
  };

  const displayScore = hoveredScore ?? score;
  const category = displayScore !== null ? getNpsCategory(displayScore) : null;
  const catConfig = category ? NPS_CAT_CONFIG[category] : null;

  if (submitted) {
    return (
      <div className={`bg-gradient-to-r from-ok-50 to-brand-50 border border-ok-200 rounded-2xl p-6 text-center ${className}`}>
        <div className="text-4xl mb-3">🎉</div>
        <div className="text-[16px] font-bold text-ink-800 mb-1">感谢您的反馈！</div>
        <div className="text-[13px] text-ink-500">
          您的评价将帮助我们不断改进训练体验
        </div>
      </div>
    );
  }

  return (
    <div className={`bg-white rounded-2xl border border-ink-100 p-6 ${className}`}>
      <div className="text-center mb-5">
        <h3 className="text-[15px] font-bold text-ink-800 mb-1">这次训练有价值吗？</h3>
        <p className="text-[12px] text-ink-500">您的反馈将帮助我们改进训练内容</p>
      </div>

      {/* 第一步：快速评价 */}
      <div className="flex items-center justify-center gap-4 mb-5">
        <button
          onClick={() => handleValueClick("valuable")}
          className={`
            flex flex-col items-center gap-2 px-8 py-4 rounded-xl border-2 transition-all
            ${valueFeedback === "valuable"
              ? "border-ok-400 bg-ok-50 scale-105"
              : "border-ink-100 hover:border-ok-200 hover:bg-ok-50/30"
            }
          `}
        >
          <span className="text-3xl">👍</span>
          <span className={`text-[13px] font-medium ${
            valueFeedback === "valuable" ? "text-ok-600" : "text-ink-600"
          }`}>
            有价值
          </span>
        </button>

        <button
          onClick={() => handleValueClick("not_valuable")}
          className={`
            flex flex-col items-center gap-2 px-8 py-4 rounded-xl border-2 transition-all
            ${valueFeedback === "not_valuable"
              ? "border-danger-400 bg-danger-50 scale-105"
              : "border-ink-100 hover:border-danger-200 hover:bg-danger-50/30"
            }
          `}
        >
          <span className="text-3xl">👎</span>
          <span className={`text-[13px] font-medium ${
            valueFeedback === "not_valuable" ? "text-danger-600" : "text-ink-600"
          }`}>
            没价值
          </span>
        </button>
      </div>

      {/* 第二步：NPS 评分（选择快速评价后显示） */}
      {valueFeedback && (
        <div className="border-t border-ink-100 pt-5 mb-5">
          <div className="text-center mb-4">
            <div className="text-[13px] font-medium text-ink-700 mb-1">
              您有多大可能向朋友推荐淬火平台？
            </div>
            {displayScore !== null && catConfig && (
              <div className={`text-[12px] font-medium ${catConfig.color}`}>
                {NPS_SCORE_LABELS[displayScore]}
              </div>
            )}
          </div>

          {/* 分数选择器 */}
          <div className="flex items-center justify-center gap-1.5 mb-4">
            {Array.from({ length: 11 }, (_, i) => i).map((s) => {
              const cat = getNpsCategory(s);
              const cfg = NPS_CAT_CONFIG[cat];
              const isSelected = score === s;
              const isHovered = hoveredScore === s;
              const inRange = displayScore !== null && s <= displayScore;

              return (
                <button
                  key={s}
                  onClick={() => handleScoreClick(s)}
                  onMouseEnter={() => setHoveredScore(s)}
                  onMouseLeave={() => setHoveredScore(null)}
                  className={`
                    w-8 h-8 rounded-lg text-[12px] font-bold transition-all
                    ${isSelected || isHovered
                      ? `${cfg.bg} ${cfg.color} scale-110`
                      : inRange
                        ? `${cfg.bg} ${cfg.color} opacity-60`
                        : "bg-ink-50 text-ink-500 hover:bg-ink-100"
                    }
                  `}
                >
                  {s}
                </button>
              );
            })}
          </div>

          {/* 图例 */}
          <div className="flex items-center justify-center gap-4 text-[10px] text-ink-400">
            <span>0 · 完全不推荐</span>
            <span className="text-ink-300">|</span>
            <span>10 · 非常推荐</span>
          </div>
        </div>
      )}

      {/* 第三步：开放反馈（评分后显示） */}
      {score !== null && (
        <div className="border-t border-ink-100 pt-5">
          <div className="text-[13px] font-medium text-ink-700 mb-2">
            您有什么建议或想说的？<span className="text-ink-400 font-normal">（选填）</span>
          </div>
          <textarea
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="告诉我们您的想法，帮助我们做得更好..."
            rows={3}
            maxLength={500}
            className="w-full px-3 py-2 rounded-xl border border-ink-200 text-[13px] text-ink-700 focus:outline-none focus:ring-2 focus:ring-brand-500/20 focus:border-brand-500 resize-none"
          />
          <div className="flex items-center justify-between mt-2">
            <span className="text-[11px] text-ink-400">{comment.length}/500</span>
            <button
              onClick={handleSubmit}
              disabled={submitting}
              className="px-5 py-2 rounded-xl bg-brand-500 hover:bg-brand-600 disabled:bg-brand-300 text-white text-[13px] font-medium transition-colors"
            >
              {submitting ? "提交中..." : "提交反馈"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export default NpsSurvey;
