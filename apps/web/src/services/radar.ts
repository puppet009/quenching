// 双维能力雷达相关类型与工具函数（T3-27）
// 对应后端 score.Result + user_profiles.skill_radar

/** 技术维雷达维度（排障技术能力） */
export const TECH_DIMENSIONS = [
  "fault_diagnosis",  // 故障诊断
  "tool_proficiency", // 工具熟练度
  "info_retrieval",   // 信息检索
  "operation_cost",   // 操作代价
  "verification",     // 验证习惯
  "time_efficiency",  // 时间效率
] as const;

export type TechDimension = typeof TECH_DIMENSIONS[number];

/** 协作维雷达维度（AI 协作能力） */
export const COLLAB_DIMENSIONS = [
  "question_quality",  // 提问质量
  "info_utilization",  // 信息利用率
  "judgment",          // 判断力
  "pitfall_awareness", // 坑点意识
  "verification_habit",// 验证习惯
  "independence",      // 独立性
] as const;

export type CollabDimension = typeof COLLAB_DIMENSIONS[number];

/** 维度显示名映射 */
export const DIMENSION_LABELS: Record<string, string> = {
  // 技术维
  fault_diagnosis: "故障诊断",
  tool_proficiency: "工具熟练度",
  info_retrieval: "信息检索",
  operation_cost: "操作代价",
  verification: "验证习惯",
  time_efficiency: "时间效率",
  // 协作维
  question_quality: "提问质量",
  info_utilization: "信息利用率",
  judgment: "判断力",
  pitfall_awareness: "坑点意识",
  verification_habit: "验证习惯",
  independence: "独立性",
};

/** 单项维度得分（0-100） */
export interface DimensionScore {
  key: string;
  label: string;
  value: number;
}

/** 雷达图数据 */
export interface RadarData {
  /** 维度列表（决定顺序和数量） */
  dimensions: string[];
  /** 得分（与 dimensions 一一对应，0-100） */
  values: number[];
}

/** 双维能力雷达完整数据 */
export interface DualDimensionRadar {
  /** 本次训练 - 技术维 */
  currentTech: RadarData;
  /** 本次训练 - 协作维 */
  currentCollab: RadarData;
  /** 历史平均 - 技术维 */
  avgTech?: RadarData;
  /** 历史平均 - 协作维 */
  avgCollab?: RadarData;
  /** 技术总分 0-100 */
  techOverall: number;
  /** 协作总分 0-100（无协作数据时为 0） */
  collabOverall: number;
  /** 是否有协作数据 */
  hasCollab: boolean;
  /** 综合分 0-100 */
  overall: number;
  /** 协作等级 C1-C5 */
  level: string;
}

/** 协作等级说明 */
export const COLLAB_LEVELS: Record<string, { label: string; desc: string; color: string }> = {
  C5: { label: "C5 · 领航者", desc: "AI 协作的顶尖水平，批判性思维极强", color: "text-brand-600" },
  C4: { label: "C4 · 驾驭者", desc: "能熟练驾驭 AI，独立判断能力强", color: "text-brand-500" },
  C3: { label: "C3 · 协作者", desc: "与 AI 配合良好，有基本的判断力", color: "text-warn-500" },
  C2: { label: "C2 · 跟随者", desc: "依赖 AI 建议，验证意识不足", color: "text-warn-400" },
  C1: { label: "C1 · 新手", desc: "刚开始使用 AI 协作，需要培养判断力", color: "text-ink-400" },
};

/** 根据综合分计算协作等级（阈值与后端 session_service 保持一致） */
export function calcCollabLevel(score: number): string {
  if (score >= 85) return "C5";
  if (score >= 70) return "C4";
  if (score >= 55) return "C3";
  if (score >= 40) return "C2";
  return "C1";
}

/** 构造技术维雷达数据（从各子分） */
export function buildTechRadar(scores: Partial<Record<TechDimension, number>>): RadarData {
  const values = TECH_DIMENSIONS.map((d) => Math.max(0, Math.min(100, scores[d] ?? 0)));
  return { dimensions: [...TECH_DIMENSIONS], values };
}

/** 构造协作维雷达数据（从各子分） */
export function buildCollabRadar(scores: Partial<Record<CollabDimension, number>>): RadarData {
  const values = COLLAB_DIMENSIONS.map((d) => Math.max(0, Math.min(100, scores[d] ?? 0)));
  return { dimensions: [...COLLAB_DIMENSIONS], values };
}

/** 计算雷达图平均分 */
export function radarAverage(data: RadarData): number {
  if (data.values.length === 0) return 0;
  const sum = data.values.reduce((a, b) => a + b, 0);
  return Math.round(sum / data.values.length);
}
