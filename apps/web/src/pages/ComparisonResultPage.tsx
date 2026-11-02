import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { SkillRadar } from "../components/SkillRadar";
import {
  DIMENSION_LABELS,
  radarAverage,
  calcCollabLevel,
  COLLAB_LEVELS,
} from "../services/radar";
import {
  calcComparisonMetrics,
  generateAISummary,
  fetchComparison,
  streamComparisonSummary,
  type ComparisonReport,
  type ComparisonMetric,
} from "../services/comparison";

/**
 * 人机对照结果页（T3-28）
 *
 * 功能：
 * - 对比纯人工 vs AI 协作两次训练的结果
 * - 核心指标：速度 / 正确率 / 危险操作 / 验证行为
 * - 技术维雷达对比图
 * - 协作维雷达（仅 AI 协作模式）
 * - AI 帮助总结（自然语言分析）
 * - 入口引导（开始对比训练 / 查看报告）
 *
 * 访问路径: /comparison-result
 */
export function ComparisonResultPage() {
  const navigate = useNavigate();
  const [report, setReport] = useState<ComparisonReport | null>(null);
  const [loading, setLoading] = useState(true);
  /** 无配对：用户尚未完成人工 + AI 协作各一次 */
  const [noPair, setNoPair] = useState(false);
  /** AI 正在思考/输出（含思考阶段） */
  const [aiThinking, setAiThinking] = useState(false);
  /** 总结来源：ai = ai-service，rule = 规则降级 */
  const [summarySource, setSummarySource] = useState<"ai" | "rule">("ai");

  // 1) 拉对照数据；2) 成功后请求 ai-service 总结流，失败降级规则文案
  useEffect(() => {
    let cancelled = false;
    let controller: AbortController | null = null;

    (async () => {
      let data: ComparisonReport;
      try {
        data = await fetchComparison();
        if (cancelled) return;
        setReport(data);
      } catch {
        if (!cancelled) setNoPair(true);
        setLoading(false);
        return;
      }
      setLoading(false);

      // 真总结流：delta 实时累加到 aiSummary
      let acc = "";
      let settled = false;
      const fallback = (data: ComparisonReport) => {
        if (settled || cancelled) return;
        settled = true;
        setSummarySource("rule");
        setAiThinking(false);
        setReport({ ...data, aiSummary: generateAISummary(data) });
      };

      controller = streamComparisonSummary({
        onThinking: () => {
          if (!cancelled) setAiThinking(true);
        },
        onDelta: (delta) => {
          if (cancelled) return;
          acc += delta;
          setAiThinking(false);
          setReport((prev) =>
            prev ? { ...prev, aiSummary: acc } : prev,
          );
        },
        onDone: () => {
          if (settled || cancelled) return;
          settled = true;
          setAiThinking(false);
          if (!acc) {
            // ai-service 空内容也按降级处理，保证有文案
            setReport((prev) => {
              if (!prev) return prev;
              return { ...prev, aiSummary: generateAISummary(prev) };
            });
            setSummarySource("rule");
          }
        },
        onError: () => {
          if (settled || cancelled) return;
          // 已有实质输出则保留并按 AI 内容收尾，否则降级规则文案
          if (acc.trim()) {
            settled = true;
            setAiThinking(false);
          } else {
            fallback(data);
          }
        },
      });
    })();

    return () => {
      cancelled = true;
      controller?.abort();
    };
  }, []);

  const metrics = useMemo(
    () => (report ? calcComparisonMetrics(report) : []),
    [report],
  );

  if (loading) {
    return (
      <div className="min-h-screen bg-ink-50 flex items-center justify-center">
        <div className="text-ink-400">加载中...</div>
      </div>
    );
  }

  if (noPair || !report) {
    return (
      <div className="min-h-screen bg-ink-50 flex items-center justify-center px-6">
        <div className="max-w-md w-full bg-white rounded-2xl border border-ink-100 shadow-sm p-8 text-center">
          <div className="text-4xl mb-4">🎯</div>
          <h2 className="text-lg font-bold text-ink-900 mb-2">
            还没有可对照的训练
          </h2>
          <p className="text-sm text-ink-500 leading-relaxed mb-6">
            人机对照需要各完成一次<strong className="text-ink-700">纯人工</strong>与
            <strong className="text-ink-700">AI 协作</strong>训练，完成后即可查看对比分析。
          </p>
          <div className="flex flex-col gap-3">
            <button
              onClick={() => navigate("/projects")}
              className="h-11 px-6 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors shadow-md shadow-brand-500/20"
            >
              去开始训练
            </button>
            <button
              onClick={() => navigate("/profile")}
              className="h-11 px-6 rounded-xl border border-ink-200 text-ink-600 text-sm font-semibold hover:bg-ink-50 transition-colors"
            >
              返回个人中心
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gradient-to-b from-brand-50/50 to-ink-50">
      {/* 顶部 Hero 区 */}
      <div className="max-w-5xl mx-auto px-6 pt-10 pb-6">
        <div className="text-center mb-8">
          <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full bg-brand-100 text-brand-700 text-xs font-semibold mb-4">
            <span>🎯</span>
            人机对照报告
          </div>
          <h1 className="text-2xl font-bold text-ink-900 mb-2">
            {report.manual.defectName}
          </h1>
          <p className="text-ink-500 text-sm">
            纯人工 vs AI 协作 · 两次训练对比分析
          </p>
        </div>

        {/* 提升幅度大卡片 */}
        <div className="bg-white rounded-2xl border border-ink-100 shadow-lg p-6 mb-6">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <BigStat
              label="综合得分提升"
              value={report.improvementPercent}
              suffix="%"
              icon="📈"
              highlight
            />
            <BigStat
              label="排障速度提升"
              value={calcSpeedImprovement(report)}
              suffix="%"
              icon="⚡"
            />
            <BigStat
              label="危险操作减少"
              value={calcDangerReduction(report)}
              suffix="%"
              icon="🛡️"
            />
          </div>
        </div>
      </div>

      <div className="max-w-5xl mx-auto px-6 pb-10 space-y-6">
        {/* 核心指标对比 */}
        <section className="bg-white rounded-2xl border border-ink-100 shadow-sm p-6">
          <h2 className="text-base font-bold text-ink-900 mb-5 flex items-center gap-2">
            <span>📊</span> 核心指标对比
          </h2>
          <div className="space-y-4">
            {metrics.map((m) => (
              <MetricBar key={m.label} metric={m} />
            ))}
          </div>
        </section>

        {/* 技术维雷达对比 */}
        <section className="bg-white rounded-2xl border border-ink-100 shadow-sm p-6">
          <h2 className="text-base font-bold text-ink-900 mb-5 flex items-center gap-2">
            <span>🎯</span> 技术能力对比
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            {/* 雷达图对比 */}
            <div className="flex flex-col items-center">
              <div className="flex items-center gap-4 mb-3">
                <LegendItem color="148, 163, 184" label="纯人工" />
                <LegendItem color="139, 92, 246" label="AI 协作" />
              </div>
              <SkillRadar
                data={report.techRadar.aiCollab}
                compareData={report.techRadar.manual}
                color="139, 92, 246"
                compareColor="148, 163, 184"
                size={280}
                labelSize={12}
              />
              <div className="mt-3 text-sm text-ink-500">
                技术分：
                <span className="text-ink-400 line-through mx-1">
                  {Math.round(radarAverage(report.techRadar.manual))}
                </span>
                <span className="font-bold text-brand-600 ml-1">
                  {Math.round(radarAverage(report.techRadar.aiCollab))}
                </span>
                <span className="text-ok-600 ml-2 text-xs">
                  +{Math.round(
                    radarAverage(report.techRadar.aiCollab) -
                      radarAverage(report.techRadar.manual),
                  )}
                </span>
              </div>
            </div>

            {/* 分项详情 */}
            <div className="space-y-3">
              <h3 className="text-sm font-semibold text-ink-700 mb-2">分项提升</h3>
              {report.techRadar.aiCollab.dimensions.map((dim, idx) => {
                const manualVal = report.techRadar.manual.values[idx];
                const aiVal = report.techRadar.aiCollab.values[idx];
                const diff = Math.round(aiVal - manualVal);
                return (
                  <div key={dim} className="flex items-center gap-3">
                    <span className="text-xs text-ink-500 w-20 flex-shrink-0">
                      {DIMENSION_LABELS[dim] || dim}
                    </span>
                    <div className="flex-1 h-2 bg-ink-100 rounded-full overflow-hidden">
                      <div
                        className="h-full bg-brand-500 rounded-full transition-all"
                        style={{ width: `${aiVal}%` }}
                      />
                    </div>
                    <span
                      className={`text-xs font-semibold w-12 text-right ${
                        diff > 0 ? "text-ok-600" : diff < 0 ? "text-danger-500" : "text-ink-400"
                      }`}
                    >
                      {diff > 0 ? "+" : ""}
                      {diff}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>
        </section>

        {/* 协作维雷达（仅 AI 协作模式） */}
        <section className="bg-white rounded-2xl border border-ink-100 shadow-sm p-6">
          <h2 className="text-base font-bold text-ink-900 mb-5 flex items-center gap-2">
            <span>🤝</span> AI 协作能力
            <span className="text-xs font-normal text-ink-400 ml-2">仅 AI 协作模式</span>
          </h2>
          <div className="flex flex-col md:flex-row items-center gap-6">
            <div className="flex-shrink-0">
              <SkillRadar
                data={report.collabRadar}
                color="16, 185, 129"
                size={240}
                labelSize={12}
              />
            </div>
            <div className="flex-1 w-full">
              <div className="mb-4">
                <div className="text-sm text-ink-500 mb-1">协作总分</div>
                <div className="flex items-baseline gap-3">
                  <span className="text-3xl font-bold text-ok-600">
                    {Math.round(radarAverage(report.collabRadar))}
                  </span>
                  <span className="text-sm text-ink-400">/ 100</span>
                </div>
              </div>
              <div className="p-3 bg-ok-50 rounded-xl border border-ok-100 mb-4">
                <div className={`text-sm font-bold ${COLLAB_LEVELS[calcCollabLevel(radarAverage(report.collabRadar))].color}`}>
                  {COLLAB_LEVELS[calcCollabLevel(radarAverage(report.collabRadar))].label}
                </div>
                <div className="text-xs text-ink-500 mt-0.5">
                  {COLLAB_LEVELS[calcCollabLevel(radarAverage(report.collabRadar))].desc}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div className="flex justify-between">
                  <span className="text-ink-400">AI 提问</span>
                  <span className="font-semibold text-ink-700">
                    {report.aiCollab.aiQuestions} 次
                  </span>
                </div>
                <div className="flex justify-between">
                  <span className="text-ink-400">坑点识别</span>
                  <span className="font-semibold text-ink-700">
                    {report.aiCollab.pitfallRecognized}/
                    {report.aiCollab.pitfallTotal}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* AI 总结 */}
        <section className="bg-white rounded-2xl border border-ink-100 shadow-sm overflow-hidden">
          <div className="px-6 py-4 border-b border-ink-100 bg-gradient-to-r from-brand-50 to-ok-50">
            <h2 className="text-base font-bold text-ink-900 flex items-center gap-2">
              <span className="text-xl">🤖</span> AI 对比分析
              <span className="text-xs font-normal text-ink-400 ml-1">
                {summarySource === "ai" ? "ai-service 智能生成" : "规则文案（AI 服务不可用时降级）"}
              </span>
            </h2>
          </div>
          <div className="p-6">
            {aiThinking && !report.aiSummary && (
              <div className="flex items-center gap-2 text-sm text-ink-400">
                <span className="inline-block w-2 h-2 rounded-full bg-brand-400 animate-pulse" />
                AI 正在分析两次训练数据...
              </div>
            )}
            <div className="prose prose-sm max-w-none text-ink-600 leading-relaxed">
              {report.aiSummary.split("\n\n").filter(Boolean).map((para, i) => (
                <p key={i} className="mb-3 last:mb-0">
                  {renderRichText(para)}
                </p>
              ))}
            </div>
          </div>
        </section>

        {/* 底部操作栏 */}
        <div className="flex flex-col sm:flex-row gap-3 justify-center pt-2">
          <button
            onClick={() => navigate("/projects")}
            className="h-11 px-6 rounded-xl bg-brand-500 hover:bg-brand-600 text-white text-sm font-semibold transition-colors shadow-md shadow-brand-500/20"
          >
            再测一次
          </button>
          <button
            onClick={() => navigate("/profile")}
            className="h-11 px-6 rounded-xl border border-ink-200 text-ink-600 text-sm font-semibold hover:bg-ink-50 transition-colors"
          >
            返回个人中心
          </button>
        </div>
      </div>
    </div>
  );
}

/* ===== 子组件 ===== */

function BigStat({
  label,
  value,
  suffix,
  icon,
  highlight = false,
}: {
  label: string;
  value: number;
  suffix: string;
  icon: string;
  highlight?: boolean;
}) {
  return (
    <div
      className={`text-center p-4 rounded-xl ${
        highlight ? "bg-brand-50 border border-brand-200" : "bg-ink-50"
      }`}
    >
      <div className="text-2xl mb-2">{icon}</div>
      <div
        className={`text-3xl font-bold ${
          highlight ? "text-brand-600" : "text-ink-800"
        }`}
      >
        {Math.abs(value)}
        <span className="text-lg ml-0.5">{suffix}</span>
      </div>
      <div className={`text-xs mt-1 ${highlight ? "text-brand-600" : "text-ink-500"}`}>
        {label}
      </div>
    </div>
  );
}

function MetricBar({ metric }: { metric: ComparisonMetric }) {
  const maxVal = Math.max(metric.manualValue, metric.aiValue, 1);
  const manualWidth = (metric.manualValue / maxVal) * 100;
  const aiWidth = (metric.aiValue / maxVal) * 100;

  const trendColor = metric.trend === "up" ? "text-ok-600" : metric.trend === "down" ? "text-danger-500" : "text-ink-400";
  const trendIcon = metric.trend === "up" ? "↑" : metric.trend === "down" ? "↓" : "→";

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <span className="text-sm font-medium text-ink-700">{metric.label}</span>
        <span className={`text-xs font-semibold ${trendColor}`}>
          {trendIcon} {metric.diffText}
        </span>
      </div>
      <div className="space-y-1.5">
        <div className="flex items-center gap-3">
          <span className="text-xs text-ink-400 w-16 flex-shrink-0">纯人工</span>
          <div className="flex-1 h-2 bg-ink-100 rounded-full overflow-hidden">
            <div
              className="h-full bg-ink-300 rounded-full transition-all"
              style={{ width: `${manualWidth}%` }}
            />
          </div>
          <span className="text-xs font-semibold text-ink-600 w-14 text-right">
            {metric.manualValue} {metric.unit}
          </span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-ink-400 w-16 flex-shrink-0">AI 协作</span>
          <div className="flex-1 h-2 bg-ink-100 rounded-full overflow-hidden">
            <div
              className="h-full bg-brand-500 rounded-full transition-all"
              style={{ width: `${aiWidth}%` }}
            />
          </div>
          <span className="text-xs font-semibold text-brand-600 w-14 text-right">
            {metric.aiValue} {metric.unit}
          </span>
        </div>
      </div>
    </div>
  );
}

function LegendItem({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5 text-xs text-ink-500">
      <span
        className="w-3 h-3 rounded-full"
        style={{ backgroundColor: `rgb(${color})` }}
      />
      {label}
    </div>
  );
}

/* ===== 工具函数 ===== */

function calcSpeedImprovement(report: ComparisonReport): number {
  if (report.manual.totalTimeSec === 0) return 0;
  const diff = report.manual.totalTimeSec - report.aiCollab.totalTimeSec;
  return Math.round((diff / report.manual.totalTimeSec) * 100);
}

function calcDangerReduction(report: ComparisonReport): number {
  if (report.manual.dangerousOps === 0) return 0;
  const diff = report.manual.dangerousOps - report.aiCollab.dangerousOps;
  return Math.max(0, Math.round((diff / report.manual.dangerousOps) * 100));
}

/** 简单的富文本渲染：支持 **加粗** */
function renderRichText(text: string) {
  const parts = text.split(/(\*\*[^*]+\*\*)/g);
  return parts.map((part, i) => {
    if (part.startsWith("**") && part.endsWith("**")) {
      return (
        <strong key={i} className="font-semibold text-ink-800">
          {part.slice(2, -2)}
        </strong>
      );
    }
    return <span key={i}>{part}</span>;
  });
}
