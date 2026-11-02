// 人机对照报告相关类型与工具函数（T3-28）
// 对比纯人工训练 vs AI协作训练的各项指标

import { http } from "@quenching/shared";
import { radarAverage, type RadarData } from "./radar";
import type { StreamEvent } from "./chat";

/** 单次训练的基础指标 */
export interface TrainingMetrics {
  /** 训练ID */
  sessionId: number;
  /** 缺陷名称 */
  defectName: string;
  /** 是否成功修复 */
  success: boolean;
  /** 总耗时（秒） */
  totalTimeSec: number;
  /** 技术分 0-100 */
  techScore: number;
  /** 协作分 0-100（纯人工为 0） */
  collabScore: number;
  /** 危险操作次数 */
  dangerousOps: number;
  /** 验证行为次数（主动查看指标/日志/测试等） */
  verifyActions: number;
  /** AI 提问次数（纯人工为 0） */
  aiQuestions: number;
  /** 坑点识别数（纯人工为 0） */
  pitfallRecognized: number;
  /** 坑点总数（纯人工为 0） */
  pitfallTotal: number;
}

/** 人机对照报告数据 */
export interface ComparisonReport {
  /** 纯人工训练数据 */
  manual: TrainingMetrics;
  /** AI 协作训练数据 */
  aiCollab: TrainingMetrics;

  /** 技术维雷达对比（人工 vs AI协作） */
  techRadar: {
    manual: RadarData;
    aiCollab: RadarData;
  };

  /** 协作维雷达（只有 AI 协作有） */
  collabRadar: RadarData;

  /** AI 帮助总结（自然语言，AI 生成的对比分析） */
  aiSummary: string;

  /** 提升百分比（综合分提升幅度） */
  improvementPercent: number;
}

// ---------- 后端 DTO（与 training-service ComparisonResponse 对齐） ----------

/** 后端单次训练指标（评分类字段可能因未评分而为 null） */
interface TrainingMetricsDTO {
  sessionId: number;
  defectName: string;
  success: boolean | null;
  totalTimeSec: number;
  techScore: number | null;
  collabScore: number | null;
  dangerousOps: number;
  verifyActions: number;
  aiQuestions: number;
  pitfallRecognized: number;
  pitfallTotal: number;
}

/** 后端雷达对（纯人工侧协作雷达为零值） */
interface RadarPairDTO {
  manual: RadarData;
  aiCollab: RadarData;
}

/** 后端人机对照响应（不含 AI 总结与提升百分比，由前端补充/计算） */
interface ComparisonResponseDTO {
  manual: TrainingMetricsDTO | null;
  aiCollab: TrainingMetricsDTO | null;
  techRadar: RadarPairDTO;
  collabRadar: RadarPairDTO;
}

/** 各项对比指标 */
export interface ComparisonMetric {
  label: string;
  manualValue: number;
  aiValue: number;
  unit: string;
  /** 趋势：up 表示 AI 协作更好（得分更高/时间更短等），down 相反 */
  trend: "up" | "down" | "neutral";
  /** 差异描述 */
  diffText: string;
  /** 是否是"越小越好"的指标（如耗时、危险操作） */
  lowerIsBetter?: boolean;
}

// ---------- 计算指标 ----------

/** 计算对比指标列表 */
export function calcComparisonMetrics(report: ComparisonReport): ComparisonMetric[] {
  const { manual, aiCollab } = report;

  const metrics: ComparisonMetric[] = [];

  // 1. 总耗时
  const timeDiff = manual.totalTimeSec - aiCollab.totalTimeSec;
  const timePercent = manual.totalTimeSec > 0 ? (timeDiff / manual.totalTimeSec) * 100 : 0;
  metrics.push({
    label: "排障速度",
    manualValue: manual.totalTimeSec,
    aiValue: aiCollab.totalTimeSec,
    unit: "秒",
    trend: timeDiff > 0 ? "up" : timeDiff < 0 ? "down" : "neutral",
    diffText: timeDiff > 0 ? `快 ${Math.round(timePercent)}%` : timeDiff < 0 ? `慢 ${Math.round(Math.abs(timePercent))}%` : "持平",
    lowerIsBetter: true,
  });

  // 2. 修复成功率
  const successDiff = (aiCollab.success ? 100 : 0) - (manual.success ? 100 : 0);
  metrics.push({
    label: "修复成功率",
    manualValue: manual.success ? 100 : 0,
    aiValue: aiCollab.success ? 100 : 0,
    unit: "%",
    trend: successDiff > 0 ? "up" : successDiff < 0 ? "down" : "neutral",
    diffText: successDiff > 0 ? `提升 ${successDiff}%` : successDiff < 0 ? `下降 ${Math.abs(successDiff)}%` : "持平",
  });

  // 3. 技术分
  const techDiff = aiCollab.techScore - manual.techScore;
  metrics.push({
    label: "技术得分",
    manualValue: Math.round(manual.techScore),
    aiValue: Math.round(aiCollab.techScore),
    unit: "分",
    trend: techDiff > 0 ? "up" : techDiff < 0 ? "down" : "neutral",
    diffText: techDiff > 0 ? `+${Math.round(techDiff)} 分` : techDiff < 0 ? `${Math.round(techDiff)} 分` : "持平",
  });

  // 4. 危险操作
  const dangerDiff = manual.dangerousOps - aiCollab.dangerousOps;
  const dangerPercent = manual.dangerousOps > 0 ? (dangerDiff / manual.dangerousOps) * 100 : 0;
  metrics.push({
    label: "危险操作",
    manualValue: manual.dangerousOps,
    aiValue: aiCollab.dangerousOps,
    unit: "次",
    trend: dangerDiff > 0 ? "up" : dangerDiff < 0 ? "down" : "neutral",
    diffText: dangerDiff > 0 ? `减少 ${Math.round(dangerPercent)}%` : dangerDiff < 0 ? `增加 ${Math.round(Math.abs(dangerPercent))}%` : "持平",
    lowerIsBetter: true,
  });

  // 5. 验证行为
  const verifyDiff = aiCollab.verifyActions - manual.verifyActions;
  metrics.push({
    label: "验证行为",
    manualValue: manual.verifyActions,
    aiValue: aiCollab.verifyActions,
    unit: "次",
    trend: verifyDiff > 0 ? "up" : verifyDiff < 0 ? "down" : "neutral",
    diffText: verifyDiff > 0 ? `+${verifyDiff} 次` : verifyDiff < 0 ? `${verifyDiff} 次` : "持平",
  });

  return metrics;
}

/**
 * 规则版对比总结（降级文案）。
 * ai-service 不可用、无配对或流中报错时使用，保证页面始终有分析内容。
 */
export function generateAISummary(report: ComparisonReport): string {
  const { manual, aiCollab } = report;
  const lines: string[] = [];

  const techDiff = aiCollab.techScore - manual.techScore;
  const timeDiff = manual.totalTimeSec - aiCollab.totalTimeSec;
  const timePercent = manual.totalTimeSec > 0 ? Math.round((timeDiff / manual.totalTimeSec) * 100) : 0;
  const dangerDiff = manual.dangerousOps - aiCollab.dangerousOps;

  // 总体评价
  if (techDiff > 10) {
    lines.push("🎉 **总体表现显著提升**！在 AI 协作模式下，你的技术得分有明显进步。");
  } else if (techDiff > 0) {
    lines.push("👍 **表现有所提升**。AI 协作帮助你在某些维度取得了进步。");
  } else if (techDiff === 0) {
    lines.push("🤔 **两次表现接近**。AI 协作没有明显改变你的技术得分，这可能是因为故障难度较低。");
  } else {
    lines.push("⚠️ **AI 协作反而得分略低**。这可能是因为你受到了 AI 错误建议的干扰，是正常的学习过程。");
  }

  // 速度分析
  if (timeDiff > 60) {
    lines.push(`⏱️ **排障速度提升明显**：AI 协作比纯人工快了约 ${timePercent}%，节省了 ${Math.round(timeDiff / 60)} 分钟。AI 帮助你更快定位问题方向。`);
  } else if (timeDiff > 0) {
    lines.push(`⏱️ **排障速度有所加快**：AI 协作模式下快了 ${timePercent}%。`);
  } else if (timeDiff < 0) {
    lines.push(`⏱️ **排障速度略有下降**：AI 协作模式下耗时略多。这可能是因为你花时间验证 AI 建议，这反而是好的习惯。`);
  }

  // 正确率分析
  if (aiCollab.success && !manual.success) {
    lines.push("✅ **修复成功率提升**：纯人工模式下未能成功修复，而 AI 协作模式下成功解决了问题。AI 提供的方向建议起到了关键作用。");
  } else if (aiCollab.success && manual.success) {
    lines.push("✅ **两次都成功修复了问题**，说明你已经掌握了这类故障的排查方法。");
  }

  // 危险操作分析
  if (dangerDiff > 0) {
    lines.push(`🛡️ **危险操作减少**：AI 协作模式下危险操作减少了 ${dangerDiff} 次，说明 AI 帮助你避免了一些盲目操作。`);
  } else if (dangerDiff < 0) {
    lines.push(`⚠️ **危险操作增加了 ${Math.abs(dangerDiff)} 次**：可能是因为你采纳了 AI 的错误建议。建议在执行操作前多加验证。`);
  }

  // 坑点分析（如果有）
  if (aiCollab.pitfallTotal > 0) {
    const recogRate = aiCollab.pitfallTotal > 0
      ? Math.round((aiCollab.pitfallRecognized / aiCollab.pitfallTotal) * 100)
      : 0;
    lines.push(`🎯 **坑点识别率 ${recogRate}%**：本次训练中 AI 共设置了 ${aiCollab.pitfallTotal} 个坑点，你成功识别了 ${aiCollab.pitfallRecognized} 个。${recogRate >= 60 ? "表现不错！" : "还需要加强对 AI 错误建议的警惕性。"}`);
  }

  // 建议
  lines.push("");
  lines.push("💡 **改进建议**：");
  if (techDiff < 20) {
    lines.push("- 尝试更多地利用 AI 做信息检索和思路发散，而不仅仅是直接要答案");
    lines.push("- 对 AI 的建议保持批判性思维，重要操作前先验证");
  }
  if (aiCollab.pitfallTotal > 0 && aiCollab.pitfallRecognized < aiCollab.pitfallTotal) {
    lines.push("- 重点回顾未识别的坑点，加深对 AI 常见错误模式的免疫力");
  }
  lines.push("- 建议多做几次对比训练，观察自己的进步趋势");

  return lines.join("\n\n");
}

// ---------- 真实接口 ----------

/** 拉取人机对照数据（无配对时后端返回业务错误，调用方据此展示引导态） */
export async function fetchComparison(): Promise<ComparisonReport> {
  const data = await http.get<unknown, ComparisonResponseDTO>("/training/comparison");
  return adaptComparison(data);
}

/** 后端响应 → 前端报告：空值兜底、协作雷达取 AI 侧、计算提升百分比 */
function adaptComparison(data: ComparisonResponseDTO): ComparisonReport {
  const manual = toMetrics(data.manual);
  const aiCollab = toMetrics(data.aiCollab);

  const manualAvg = radarAverage(data.techRadar.manual);
  const aiAvg = radarAverage(data.techRadar.aiCollab);
  const improvementPercent = manualAvg > 0
    ? Math.round(((aiAvg - manualAvg) / manualAvg) * 100)
    : 0;

  return {
    manual,
    aiCollab,
    techRadar: {
      manual: data.techRadar.manual,
      aiCollab: data.techRadar.aiCollab,
    },
    // 纯人工无协作维度，前端只展示 AI 协作侧
    collabRadar: data.collabRadar.aiCollab,
    aiSummary: "",
    improvementPercent,
  };
}

/** DTO 空值兜底为前端非空类型 */
function toMetrics(dto: TrainingMetricsDTO | null): TrainingMetrics {
  return {
    sessionId: dto?.sessionId ?? 0,
    defectName: dto?.defectName ?? "",
    success: dto?.success ?? false,
    totalTimeSec: dto?.totalTimeSec ?? 0,
    techScore: dto?.techScore ?? 0,
    collabScore: dto?.collabScore ?? 0,
    dangerousOps: dto?.dangerousOps ?? 0,
    verifyActions: dto?.verifyActions ?? 0,
    aiQuestions: dto?.aiQuestions ?? 0,
    pitfallRecognized: dto?.pitfallRecognized ?? 0,
    pitfallTotal: dto?.pitfallTotal ?? 0,
  };
}

/**
 * 请求人机对照 AI 总结 SSE（ai-service 生成）。
 * 无业务 body；返回 AbortController 用于取消。
 * 任一失败由 onError 通知，调用方降级为规则文案。
 */
export function streamComparisonSummary(callbacks: {
  onDelta?: (delta: string, event: StreamEvent) => void;
  onDone?: (event: StreamEvent) => void;
  onError?: (error: string) => void;
  onThinking?: () => void;
}): AbortController {
  const controller = new AbortController();

  (async () => {
    try {
      const token = localStorage.getItem("token");
      const resp = await fetch("/api/v1/training/comparison/summary", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({}),
        signal: controller.signal,
      });

      if (!resp.ok) {
        const errText = await resp.text().catch(() => "");
        callbacks.onError?.(`请求失败（${resp.status}）${errText ? `：${errText}` : ""}`);
        return;
      }

      const reader = resp.body?.getReader();
      if (!reader) {
        callbacks.onError?.("浏览器不支持流式响应");
        return;
      }

      const decoder = new TextDecoder("utf-8");
      let buffer = "";

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // 解析 SSE 帧：data: {...}\n\n
        const frames = buffer.split("\n\n");
        buffer = frames.pop() ?? "";

        for (const frame of frames) {
          const trimmed = frame.trim();
          if (!trimmed.startsWith("data:")) continue;

          const jsonStr = trimmed.slice(5).trim();
          if (!jsonStr) continue;

          try {
            const event: StreamEvent = JSON.parse(jsonStr);
            switch (event.type) {
              case "thinking":
                callbacks.onThinking?.();
                break;
              case "delta":
                if (event.delta) callbacks.onDelta?.(event.delta, event);
                break;
              case "done":
                callbacks.onDone?.(event);
                break;
              case "error":
                callbacks.onError?.(event.error ?? "AI 响应错误");
                break;
            }
          } catch {
            // 解析失败跳过，不中断流
          }
        }
      }
    } catch (err: unknown) {
      if ((err as { name?: string })?.name === "AbortError") {
        return; // 用户主动取消
      }
      callbacks.onError?.((err as Error)?.message ?? "网络错误");
    }
  })();

  return controller;
}
