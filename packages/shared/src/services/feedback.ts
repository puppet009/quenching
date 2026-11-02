// NPS 评价与反馈服务（T5-9）
//
// 功能：
//   - 提交快速评价（有价值/没价值）
//   - 提交 NPS 评分（0-10）+ 开放反馈
//   - 后台查看反馈列表
//   - NPS 分数统计

import { http } from "./http";

export type FeedbackValue = "valuable" | "not_valuable";

export interface NpsFeedback {
  id: number;
  user_id: number;
  user_name: string;
  training_id: number;
  project_name: string;
  defect_name: string;
  score: number; // 0-10
  value_feedback?: FeedbackValue;
  comment?: string;
  created_at: string;
}

export interface NpsStats {
  total_count: number;
  nps_score: number | null;
  promoters: number; // 9-10分
  passives: number;  // 7-8分
  detractors: number; // 0-6分
  valuable_count: number;
  not_valuable_count: number;
  avg_score: number | null;
}

/** 提交快速评价 */
export async function submitValueFeedback(
  trainingId: number,
  value: FeedbackValue,
): Promise<{ success: boolean }> {
  return http.post("/feedback/value", { training_id: trainingId, value });
}

/** 提交 NPS 评价 */
export async function submitNpsFeedback(params: {
  training_id: number;
  score: number;
  value_feedback?: FeedbackValue;
  comment?: string;
}): Promise<{ success: boolean; id: number }> {
  return http.post("/feedback/nps", params);
}

/** 获取 NPS 反馈列表（后台） */
export async function listNpsFeedback(params: {
  page?: number;
  page_size?: number;
  score_min?: number;
  score_max?: number;
  has_comment?: boolean;
}): Promise<{
  items: NpsFeedback[];
  total: number;
  stats: NpsStats;
}> {
  return http.get<unknown, {items:NpsFeedback[];total:number;stats:NpsStats}>("/admin/feedback/nps",{params});
}

/* ===== 显示配置 ===== */

export const NPS_SCORE_LABELS: Record<number, string> = {
  0: "完全不推荐",
  1: "非常不推荐",
  2: "很不推荐",
  3: "不推荐",
  4: "不太推荐",
  5: "一般",
  6: "还算可以",
  7: "比较推荐",
  8: "推荐",
  9: "很推荐",
  10: "非常推荐",
};

export const NPS_CATEGORY = {
  promoter: { label: "推荐者", range: "9-10 分", color: "text-ok-600", bg: "bg-ok-50" },
  passive: { label: "中立者", range: "7-8 分", color: "text-warn-600", bg: "bg-warn-50" },
  detractor: { label: "贬损者", range: "0-6 分", color: "text-danger-600", bg: "bg-danger-50" },
};

export function getNpsCategory(score: number): "promoter" | "passive" | "detractor" {
  if (score >= 9) return "promoter";
  if (score >= 7) return "passive";
  return "detractor";
}

/* ===== Mock 数据 ===== */

export function generateMockNpsFeedbacks(): NpsFeedback[] {
  const names = ["张三", "李四", "王五", "赵六", "钱七", "孙八", "周九", "吴十", "郑十一", "王十二"];
  const projects = ["电商微服务", "社交应用", "支付系统"];
  const defects = ["CPU满载", "磁盘打满", "连接泄漏", "缓存穿透", "消息积压", "慢查询"];
  const comments = [
    "非常棒的训练平台，学到了很多！",
    "AI 协作功能很有新意，希望能增加更多故障场景。",
    "整体不错，但界面可以再优化一下。",
    "故障模拟很真实，复盘分析也很到位。",
    "希望能增加更多难度级别的训练。",
    "沙箱启动有点慢，其他都很好。",
    "复盘报告非常详细，对提升很有帮助。",
    "建议增加多人协作训练模式。",
    undefined,
    "工具链很完整，期待更多功能！",
  ];

  return Array(12).fill(0).map((_, i) => {
    const score = Math.floor(Math.random() * 11);
    return {
      id: i + 1,
      user_id: i + 1,
      user_name: names[i % names.length],
      training_id: i + 100,
      project_name: projects[i % projects.length],
      defect_name: defects[i % defects.length],
      score,
      value_feedback: score >= 7 ? "valuable" : (score <= 5 ? "not_valuable" : undefined),
      comment: comments[i],
      created_at: new Date(Date.now() - i * 3600000 * 2).toISOString(),
    };
  });
}

export function calculateNpsStats(feedbacks: NpsFeedback[]): NpsStats {
  const total = feedbacks.length;
  if (total === 0) {
    return {
      total_count: 0,
      nps_score: 0,
      promoters: 0,
      passives: 0,
      detractors: 0,
      valuable_count: 0,
      not_valuable_count: 0,
      avg_score: 0,
    };
  }

  let promoters = 0;
  let passives = 0;
  let detractors = 0;
  let valuable = 0;
  let notValuable = 0;
  let totalScore = 0;

  feedbacks.forEach((f) => {
    totalScore += f.score;
    if (f.score >= 9) promoters++;
    else if (f.score >= 7) passives++;
    else detractors++;

    if (f.value_feedback === "valuable") valuable++;
    else if (f.value_feedback === "not_valuable") notValuable++;
  });

  const nps = Math.round(((promoters - detractors) / total) * 100);
  const avg = Math.round((totalScore / total) * 10) / 10;

  return {
    total_count: total,
    nps_score: nps,
    promoters,
    passives,
    detractors,
    valuable_count: valuable,
    not_valuable_count: notValuable,
    avg_score: avg,
  };
}
