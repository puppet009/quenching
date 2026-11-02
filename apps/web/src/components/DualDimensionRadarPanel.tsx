import { SkillRadar } from "./SkillRadar";
import {
  DIMENSION_LABELS,
  COLLAB_LEVELS,
  radarAverage,
  type DualDimensionRadar,
} from "../services/radar";

interface DualDimensionRadarPanelProps {
  data: DualDimensionRadar;
  /** 是否显示历史对比 */
  showCompare?: boolean;
  /** 尺寸模式 */
  size?: "sm" | "md" | "lg";
  className?: string;
}

/**
 * 双维能力雷达面板组件（T3-27）
 *
 * 左侧技术维雷达 + 右侧协作维雷达 + 顶部核心指标
 * 支持本次 vs 历史平均对比（虚线）
 * 可在个人中心、复盘报告页等多处复用
 */
export function DualDimensionRadarPanel({
  data,
  showCompare = true,
  size = "md",
  className = "",
}: DualDimensionRadarPanelProps) {
  const radarSize = size === "sm" ? 220 : size === "lg" ? 320 : 280;
  const labelSize = size === "sm" ? 11 : size === "lg" ? 13 : 12;

  const hasCompare = showCompare && data.avgTech && data.avgCollab;

  return (
    <div className={`bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden ${className}`}>
      {/* 头部：核心指标 */}
      <div className="px-6 pt-5 pb-4 border-b border-ink-100">
        <h3 className="text-base font-bold text-ink-900 mb-4">能力雷达</h3>
        <div className="grid grid-cols-3 gap-3">
          <ScoreCard
            label="技术分"
            value={data.techOverall}
            color="text-brand-600"
            bgClass="bg-brand-50"
            compare={hasCompare ? radarAverage(data.avgTech!) : undefined}
          />
          <ScoreCard
            label="协作分"
            value={data.collabOverall}
            color="text-ok-600"
            bgClass="bg-ok-50"
            compare={hasCompare && data.hasCollab ? radarAverage(data.avgCollab!) : undefined}
            badge={data.hasCollab ? COLLAB_LEVELS[data.level]?.label : "暂无"}
          />
          <ScoreCard
            label="综合分"
            value={data.overall}
            color="text-warn-600"
            bgClass="bg-warn-50"
            compare={
              hasCompare
                ? Math.round(
                    (radarAverage(data.avgTech!) + radarAverage(data.avgCollab!)) / 2,
                  )
                : undefined
            }
          />
        </div>
      </div>

      {/* 双雷达图 */}
      <div className="p-5 grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* 技术维 */}
        <div className="flex flex-col items-center">
          <div className="flex items-center gap-2 mb-2">
            <span className="w-3 h-3 rounded-full bg-brand-500" />
            <span className="text-sm font-semibold text-ink-700">排障技术</span>
            {hasCompare && (
              <span className="text-[11px] text-ink-400 ml-2 flex items-center gap-1">
                <span className="inline-block w-4 h-0 border-t-2 border-dashed border-ink-400" />
                历史平均
              </span>
            )}
          </div>
          <SkillRadar
            data={{
              dimensions: data.currentTech.dimensions.map((d) => DIMENSION_LABELS[d] ?? d),
              values: data.currentTech.values,
            }}
            compareData={
              hasCompare
                ? {
                    dimensions: data.avgTech!.dimensions.map((d) => DIMENSION_LABELS[d] ?? d),
                    values: data.avgTech!.values,
                  }
                : undefined
            }
            color="139, 92, 246"
            compareColor="148, 163, 184"
            size={radarSize}
            labelSize={labelSize}
          />
          <div className="mt-2 text-[13px] text-ink-500">
            本次 <span className="font-semibold text-brand-600">{data.techOverall}</span> 分
            {hasCompare && (
              <>
                {" · 历史 "}
                <span className="text-ink-500">{radarAverage(data.avgTech!)}</span> 分
              </>
            )}
          </div>
        </div>

        {/* 协作维 */}
        <div className="flex flex-col items-center">
          <div className="flex items-center gap-2 mb-2">
            <span className="w-3 h-3 rounded-full bg-ok-500" />
            <span className="text-sm font-semibold text-ink-700">AI 协作</span>
            {hasCompare && (
              <span className="text-[11px] text-ink-400 ml-2 flex items-center gap-1">
                <span className="inline-block w-4 h-0 border-t-2 border-dashed border-ink-400" />
                历史平均
              </span>
            )}
          </div>
          <SkillRadar
            data={{
              dimensions: data.currentCollab.dimensions.map((d) => DIMENSION_LABELS[d] ?? d),
              values: data.currentCollab.values,
            }}
            compareData={
              hasCompare && data.hasCollab
                ? {
                    dimensions: data.avgCollab!.dimensions.map((d) => DIMENSION_LABELS[d] ?? d),
                    values: data.avgCollab!.values,
                  }
                : undefined
            }
            color="16, 185, 129"
            compareColor="148, 163, 184"
            size={radarSize}
            labelSize={labelSize}
          />
          <div className="mt-2 text-[13px] text-ink-500">
            本次{" "}
            <span className="font-semibold text-ok-600">
              {data.hasCollab ? data.collabOverall : "—"}
            </span>{" "}
            分
            {hasCompare && data.hasCollab && (
              <>
                {" · 历史 "}
                <span className="text-ink-500">{radarAverage(data.avgCollab!)}</span> 分
              </>
            )}
          </div>
        </div>
      </div>

      {/* 底部说明 */}
      {data.hasCollab && COLLAB_LEVELS[data.level] && (
        <div className="px-5 pb-4">
          <div className="flex items-center gap-3 p-3 bg-ok-50 rounded-xl border border-ok-100">
            <span className="text-2xl">🏆</span>
            <div className="flex-1">
              <div className={`text-sm font-bold ${COLLAB_LEVELS[data.level].color}`}>
                {COLLAB_LEVELS[data.level].label}
              </div>
              <div className="text-[12px] text-ink-500 mt-0.5">
                {COLLAB_LEVELS[data.level].desc}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/* ===== 子组件：分数卡片 ===== */

function ScoreCard({
  label,
  value,
  color,
  bgClass,
  compare,
  badge,
}: {
  label: string;
  value: number;
  color: string;
  bgClass: string;
  compare?: number;
  badge?: string;
}) {
  const diff = compare !== undefined ? value - compare : null;
  const diffPositive = diff !== null && diff > 0;
  const diffNegative = diff !== null && diff < 0;

  return (
    <div className={`${bgClass} rounded-xl p-3 text-center`}>
      <div className="text-[11px] text-ink-500 mb-1">{label}</div>
      <div className={`text-2xl font-bold ${color} leading-tight`}>
        {Math.round(value)}
      </div>
      {diff !== null && (
        <div
          className={`text-[11px] mt-0.5 ${
            diffPositive
              ? "text-ok-600"
              : diffNegative
                ? "text-danger-600"
                : "text-ink-400"
          }`}
        >
          {diffPositive ? "↑" : diffNegative ? "↓" : "→"} {Math.abs(Math.round(diff))} 分
        </div>
      )}
      {badge && (
        <div className="text-[10px] text-ink-500 mt-0.5 font-medium">{badge}</div>
      )}
    </div>
  );
}
