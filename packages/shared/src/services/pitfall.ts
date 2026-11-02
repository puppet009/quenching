// 坑点配置后台管理接口（T3-18/T3-19）
// 对接 training-service /api/v1/admin/pitfalls
import { http } from "./http";

/** 坑点类型 */
export type PitfallType = "causal_misjudgment" | "info_omission" | "overconfidence";

/** 坑点配置状态 */
export type PitfallConfigStatus = "draft" | "active" | "inactive";

/** 置信度等级 */
export type ConfidenceLevel = "low" | "medium" | "high" | "random";

/** 严重程度 */
export type SeverityLevel = "low" | "medium" | "high";

/** 触发时机 */
export type TriggerWhen = "always" | "question_match" | "stage_enter" | "delay";

/** 触发条件 */
export interface TriggerCondition {
  when: TriggerWhen;
  probability: number;
  max_times?: number;
  question_keywords?: string[];
  question_patterns?: string[];
  stages?: string[];
  delay_seconds?: number;
  window?: {
    start_seconds: number;
    end_seconds: number;
  };
  requires?: {
    fault_triggered?: boolean;
    min_turn?: number;
    max_turn?: number;
  };
}

/** 识别规则 */
export interface RecognitionRules {
  dispute_keywords?: string[];
  independent_verify_actions?: string[];
  ignore_and_proceed?: boolean;
}

/** 踩坑规则 */
export interface FallRules {
  adopt_actions?: string[];
  adopt_keywords?: string[];
  worsen_fault?: boolean;
  consequence?: {
    type: string;
    intensity: number;
  };
}

/** 坑点配置完整结构 */
export interface PitfallConfig {
  id: number;
  defect_id: number;
  pitfall_type: PitfallType;
  name: string;
  description?: string;
  trigger_condition: TriggerCondition;
  error_content: string;
  confidence_level: ConfidenceLevel;
  recognition_rules: RecognitionRules;
  pitfall_rules: FallRules;
  correct_approach?: string;
  severity: SeverityLevel;
  status: PitfallConfigStatus;
  sort_order: number;
  created_by?: number;
  created_at?: string;
  updated_at?: string;
}

/** 分页查询参数 */
export interface PitfallQueryParams {
  defect_id?: number;
  pitfall_type?: PitfallType;
  status?: PitfallConfigStatus;
  page?: number;
  page_size?: number;
}

/** 分页结果 */
export interface PitfallPageResult {
  total: number;
  page: number;
  page_size: number;
  items: PitfallConfig[];
}

/** 状态变更请求 */
export interface UpdateStatusRequest {
  status: PitfallConfigStatus;
}

// ---------- API 方法 ----------

/** 分页查询坑点配置列表 */
export function listPitfallConfigs(params: PitfallQueryParams = {}) {
  return http.get<unknown, PitfallPageResult>("/admin/pitfalls", { params });
}

/** 获取坑点配置详情 */
export function getPitfallConfig(id: number) {
  return http.get<unknown, PitfallConfig>(`/admin/pitfalls/${id}`);
}

/** 创建坑点配置 */
export function createPitfallConfig(data: Omit<PitfallConfig, "id" | "created_at" | "updated_at">) {
  return http.post<unknown, PitfallConfig>("/admin/pitfalls", data);
}

/** 更新坑点配置 */
export function updatePitfallConfig(id: number, data: Omit<PitfallConfig, "id" | "created_at" | "updated_at">) {
  return http.put<unknown, PitfallConfig>(`/admin/pitfalls/${id}`, data);
}

/** 更新配置状态 */
export function updatePitfallStatus(id: number, status: PitfallConfigStatus) {
  return http.patch<unknown, null>(`/admin/pitfalls/${id}/status`, { status });
}

/** 删除坑点配置（软删除） */
export function deletePitfallConfig(id: number) {
  return http.delete<unknown, null>(`/admin/pitfalls/${id}`);
}

// ---------- UI 常量 ----------

/** 坑点类型标签 */
export const PITFALL_TYPE_LABELS: Record<PitfallType, string> = {
  causal_misjudgment: "因果误判",
  info_omission: "信息遗漏",
  overconfidence: "过度自信",
};

/** 坑点类型颜色 */
export const PITFALL_TYPE_COLORS: Record<PitfallType, { bg: string; text: string; border: string }> = {
  causal_misjudgment: {
    bg: "bg-brand-50",
    text: "text-brand-700",
    border: "border-brand-200",
  },
  info_omission: {
    bg: "bg-warn-50",
    text: "text-warn-700",
    border: "border-warn-200",
  },
  overconfidence: {
    bg: "bg-danger-50",
    text: "text-danger-700",
    border: "border-danger-200",
  },
};

/** 状态标签 */
export const PITFALL_STATUS_LABELS: Record<PitfallConfigStatus, string> = {
  draft: "草稿",
  active: "已启用",
  inactive: "已禁用",
};

/** 状态颜色 */
export const PITFALL_STATUS_COLORS: Record<PitfallConfigStatus, { bg: string; text: string; dot: string }> = {
  draft: { bg: "bg-ink-100", text: "text-ink-600", dot: "bg-ink-400" },
  active: { bg: "bg-ok-50", text: "text-ok-700", dot: "bg-ok-500" },
  inactive: { bg: "bg-danger-50", text: "text-danger-600", dot: "bg-danger-400" },
};

/** 置信度标签 */
export const CONFIDENCE_LABELS: Record<ConfidenceLevel, string> = {
  low: "低",
  medium: "中",
  high: "高",
  random: "随机",
};

/** 严重程度标签 */
export const SEVERITY_LABELS: Record<SeverityLevel, string> = {
  low: "低",
  medium: "中",
  high: "高",
};

/** 触发时机标签 */
export const TRIGGER_WHEN_LABELS: Record<TriggerWhen, string> = {
  always: "始终触发",
  question_match: "提问匹配",
  stage_enter: "阶段进入",
  delay: "延迟触发",
};
