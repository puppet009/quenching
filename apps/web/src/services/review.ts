// 复盘服务层（T4-16）
//
// 真实接口全部经统一请求层 http.ts（自动注入 JWT、解包 {code,msg,data}），
// 对应后端 review-service（/api/v1/reviews）：
//   POST /generate            触发生成（幂等），body: {session_id, project_id}
//   GET  /session/:session_id 轻量状态轮询
//   GET  /history?page&size   分页历史
//   GET  /:id                 详情（Review 标量字段平铺 + sections 章节）
//   GET  /:id/sections        仅章节
//
// 文件下半部保留 T3-29 演示页（/review-demo）使用的坑点/协作面板类型，
// 与真实接口无关；新业务代码不要引用它们。

import { http } from "@quenching/shared";
import type { GrowthReport } from './growthReview';

/* ===================== 真实接口类型（对齐后端 model） ===================== */

/** 复盘生成状态 */
export type GenerationStatus = "pending" | "generating" | "completed" | "failed";

/** 复盘级别 */
export type ReviewLevel = "basic" | "method" | "arch";

/** 复盘报告（对应后端 model.Review，详情接口中字段与 sections 平铺在同一对象） */
export interface Review {
  id: number;
  session_id: number;
  user_id: number;
  project_id: number;
  title: string;
  /** 总结（Markdown） */
  summary: string;
  /** 整体评价（Markdown） */
  overall_evaluation: string;
  review_level: ReviewLevel;
  generation_status: GenerationStatus;
  ai_model: string;
  prompt_version: string;
  /** 生成耗时（毫秒） */
  generation_duration_ms: number;
  /** 技术分 0-100 */
  tech_score: number;
  /** 协作分 0-100（纯人工为 0） */
  collab_score: number;
  /** 提问质量分 */
  question_quality_score: number;
  /** 判断力分 */
  judgment_score: number;
  /** 错误识别率（0-100） */
  error_recognition_rate: number;
  /** 证据验证率（0-100） */
  evidence_validation_rate: number;
  /** AI 坑点总数 */
  ai_pitfall_count: number;
  /** 已识别坑点数 */
  ai_pitfall_recognized: number;
  retry_count: number;
  error_message: string;
  created_at: string;
  updated_at: string;
}

/** 复盘章节（content 为 Markdown 正文；content_data 为 JSONB 附加数据，可空） */
export interface ReviewSection {
  id: number;
  review_id: number;
  /** 章节类型，见 SECTION_TYPE_META */
  section_type: string;
  section_level: ReviewLevel;
  title: string;
  /** Markdown 正文 */
  content: string;
  /** 结构化附加数据（基础复盘为空，序列化为 base64 字符串或缺省） */
  content_data?: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

/** 详情接口返回：Review 字段平铺 + sections */
export interface ReviewDetail extends Review {
  growth_report?: GrowthReport;
  sections: ReviewSection[];
}

/** 状态轮询结果（轻量，不含大字段） */
export interface ReviewStatus {
  phase?: string;
  processed_sources?: number;
  total_sources?: number;
  id: number;
  session_id: number;
  generation_status: GenerationStatus;
  error_message: string;
}

/** 历史列表项（刻意不含 summary 等大字段） */
export interface ReviewListItem {
  id: number;
  session_id: number;
  title: string;
  generation_status: GenerationStatus;
  tech_score: number;
  collab_score: number;
  generation_duration_ms: number;
  created_at: string;
}

/** 分页结果 */
export interface ReviewHistoryPage {
  total: number;
  list: ReviewListItem[];
}

/** 章节类型展示元信息（与后端 sort_order/title 常量对齐） */
export const SECTION_TYPE_META: Record<
  string,
  { title: string; icon: string; sortOrder: number }
> = {
  root_cause_analysis: { title: "根因分析", icon: "🔗", sortOrder: 10 },
  fault_timeline: { title: "时间线解读", icon: "⏱️", sortOrder: 20 },
  path_comparison: { title: "路径对比", icon: "🛤️", sortOrder: 30 },
  highlights_pitfalls: { title: "亮点与坑点", icon: "🎭", sortOrder: 40 },
  methodology_summary: { title: "方法论提炼", icon: "🧭", sortOrder: 50 },
  knowledge_extension: { title: "知识点梳理", icon: "📚", sortOrder: 60 },
  assistant_changes: { title: "助手修改记录", icon: "📝", sortOrder: 70 },
};

/* ===================== 助手修改记录（M5 确定性章节） ===================== */

/** 一次文件修改记录（对齐 review-service model.ChangeItem） */
export interface ChangeItemData {
  id: number;
  pod: string;
  container?: string;
  path: string;
  /** 写入前文件是否存在（false 为新建） */
  before_existed: boolean;
  before_content?: string;
  after_content: string;
  /** 相对会话开始的秒数 */
  t: number;
}

/** 修改记录章节结构化载荷（content_data） */
export interface ChangeAnalysisData {
  total_count: number;
  changes: ChangeItemData[];
}

/**
 * 解析章节 content_data（后端 []byte 经 JSON 序列化为 base64）。
 * 缺失或非法时返回 null，由调用方退回 Markdown 渲染。
 */
export function parseSectionData<T>(contentData?: string): T | null {
  if (!contentData) return null;
  try {
    // atob 得到二进制串，经 TextDecoder 还原 UTF-8（修改内容含中文）
    const bin = atob(contentData);
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
    return JSON.parse(new TextDecoder().decode(bytes)) as T;
  } catch {
    return null;
  }
}

/** 相对秒数 → 「第 m 分 s 秒」 */
export function formatOffsetSec(sec: number): string {
  if (sec < 0) sec = 0;
  if (sec < 60) return `第 ${sec} 秒`;
  return `第 ${Math.floor(sec / 60)} 分 ${sec % 60} 秒`;
}

/** 终态判定 */
export function isTerminalStatus(status: GenerationStatus): boolean {
  return status === "completed" || status === "failed";
}

/* ===================== 真实接口 ===================== */

/** 触发生成（幂等：已有记录直接返回，终态不重复触发） */
export function generateReview(params: {
  session_id: number;
  project_id: number;
}): Promise<Review> {
  return http.post("/reviews/generate", params);
}

/** 按训练会话 ID 轮询生成状态 */
export function getReviewStatus(sessionId: number): Promise<ReviewStatus> {
  return http.get(`/reviews/session/${sessionId}`);
}

/** 获取复盘详情（含 Markdown 章节） */
export function getReviewDetail(reviewId: number): Promise<ReviewDetail> {
  return http.get(`/reviews/${reviewId}`);
}

/** 仅获取章节（长报告按需懒加载用） */
export function getReviewSections(reviewId: number): Promise<ReviewSection[]> {
  return http.get(`/reviews/${reviewId}/sections`);
}

/** 分页查询复盘历史（后端默认 page=1、size=20、size 上限 100） */
export function listReviewHistory(params: {
  page?: number;
  size?: number;
}): Promise<ReviewHistoryPage> {
  return http.get("/reviews/history", {
    params: {
      page: params.page ?? 1,
      size: params.size ?? 20,
    },
  });
}

/* ===================== 展示工具 ===================== */

/** 评分等级 */
export type ScoreGrade = "S" | "A" | "B" | "C" | "D";

export const SCORE_GRADES: Record<
  ScoreGrade,
  { label: string; color: string; bgColor: string; minScore: number }
> = {
  S: { label: "卓越", color: "text-brand-600", bgColor: "bg-brand-50", minScore: 90 },
  A: { label: "优秀", color: "text-ok-600", bgColor: "bg-ok-50", minScore: 80 },
  B: { label: "良好", color: "text-ok-500", bgColor: "bg-ok-50/50", minScore: 70 },
  C: { label: "及格", color: "text-warn-600", bgColor: "bg-warn-50", minScore: 60 },
  D: { label: "待提升", color: "text-danger-600", bgColor: "bg-danger-50", minScore: 0 },
};

/** 根据分数获取等级；未出分（0 分且报告未完成）返回 null */
export function getScoreGrade(score: number): ScoreGrade | null {
  if (score <= 0) return null;
  if (score >= 90) return "S";
  if (score >= 80) return "A";
  if (score >= 70) return "B";
  if (score >= 60) return "C";
  return "D";
}

/** 格式化时长（毫秒 → 人类可读） */
export function formatDuration(ms: number): string {
  if (!ms || ms < 0) return "—";
  const totalSec = Math.floor(ms / 1000);
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  if (min === 0) return `${sec} 秒`;
  if (sec === 0) return `${min} 分钟`;
  return `${min} 分 ${sec} 秒`;
}

/** 生成状态中文文案 */
export const GEN_STATUS_LABELS: Record<GenerationStatus, string> = {
  pending: "排队中",
  generating: "生成中",
  completed: "已完成",
  failed: "生成失败",
};

/* ======================================================================= */
/* 以下为 T3-29 演示页（/review-demo）专用类型与常量，真实业务请勿引用。       */
/* ======================================================================= */

/** 坑点类型 */
export type PitfallType = "causal_misjudgment" | "information_omission" | "overconfidence";

export const PITFALL_TYPE_LABELS: Record<PitfallType, string> = {
  causal_misjudgment: "因果误判",
  information_omission: "信息遗漏",
  overconfidence: "过度自信",
};

export const PITFALL_TYPE_COLORS: Record<
  PitfallType,
  { bg: string; text: string; border: string; dot: string }
> = {
  causal_misjudgment: { bg: "bg-brand-50", text: "text-brand-700", border: "border-brand-200", dot: "bg-brand-500" },
  information_omission: { bg: "bg-warn-50", text: "text-warn-700", border: "border-warn-200", dot: "bg-warn-500" },
  overconfidence: { bg: "bg-danger-50", text: "text-danger-700", border: "border-danger-200", dot: "bg-danger-500" },
};

export type PitfallStatus = "fell" | "recognized" | "ignored";

export const PITFALL_STATUS_LABELS: Record<PitfallStatus, string> = {
  fell: "踩坑了",
  recognized: "识别成功",
  ignored: "未受影响",
};

export const PITFALL_STATUS_COLORS: Record<
  PitfallStatus,
  { text: string; bg: string; icon: string }
> = {
  fell: { text: "text-danger-600", bg: "bg-danger-50", icon: "😵" },
  recognized: { text: "text-ok-600", bg: "bg-ok-50", icon: "🎯" },
  ignored: { text: "text-ink-500", bg: "bg-ink-100", icon: "🤷" },
};

export interface PitfallAnalysisItem {
  pitfallId: number;
  name: string;
  type: PitfallType;
  appearedAtMs: number;
  confidence: "low" | "medium" | "high";
  aiSuggestion: string;
  correctApproach: string;
  whyMisleading: string;
  status: PitfallStatus;
  userAction: string;
  improvementTip: string;
  worsened: boolean;
}

export interface PitfallReviewData {
  total: number;
  recognized: number;
  fell: number;
  ignored: number;
  recognitionRate: number;
  items: PitfallAnalysisItem[];
  hasAICollab: boolean;
}

export interface CollabThreeFactor {
  questionQuality: number;
  judgment: number;
  verificationHabit: number;
}

export interface CollabReviewData {
  overall: number;
  level: string;
  factors: CollabThreeFactor;
  radar: { dimensions: string[]; values: number[] };
  typicalQuestions: TypicalQuestionItem[];
  hasCollab: boolean;
}

export interface TypicalQuestionItem {
  type: "good" | "bad";
  question: string;
  comment: string;
}

/** 格式化相对时间（毫秒 → mm:ss），演示面板使用 */
export function formatRelativeTime(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  return `${min}:${sec.toString().padStart(2, "0")}`;
}

export function calcRecognitionRate(recognized: number, total: number): number {
  if (total === 0) return 0;
  return Math.round((recognized / total) * 100);
}

export function formatTimelineTime(ms: number): string {
  return formatRelativeTime(ms);
}
