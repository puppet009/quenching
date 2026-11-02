// 个人中心相关类型与接口（第五片：真实接口版）
// 用户资料 GET /users/me；训练统计与雷达 GET /training/sessions/overview；
// 训练历史 GET /training/sessions；风险确认 POST /users/me/risk-notice-ack。

import { http, formatIsoMinute } from "@quenching/shared";
import type { TrainingClosureStatus } from "./trainingHistory";
import {
  buildTechRadar,
  buildCollabRadar,
  type DualDimensionRadar,
} from "./radar";

/** 用户基本信息（资料来自 /users/me，训练累计来自 overview） */
export interface UserProfile {
  id: number;
  username: string;
  nickname: string;
  avatar?: string;
  /** 个人简介 */
  bio?: string;
  email?: string;
  phone?: string;
  /** 累计训练次数 */
  totalSessions: number;
  /** 累计训练时长（分钟） */
  totalMinutes: number;
  /** 注册时间 */
  createdAt: string;
  /** 协作等级 C1-C5 */
  collabLevel: string;
}

/** 训练历史记录 */
export interface TrainingHistoryItem extends TrainingClosureStatus {
  id: number;
  projectName: string;
  defectName: string;
  /** 难度: easy / medium / hard */
  difficulty: "easy" | "medium" | "hard";
  /** 模式: manual（aiMode=none）/ ai_collab（aiMode=assistant） */
  mode: "manual" | "ai_collab";
  /** 技术得分 0-100（未评分时为 undefined） */
  techScore?: number;
  /** 协作得分 0-100（manual 或未评分为 undefined） */
  collabScore?: number;
  /** 是否成功修复（未结束评分为 undefined） */
  success?: boolean;
  /** 耗时（秒） */
  durationSec: number;
  /** 训练时间（本地时间 YYYY-MM-DD HH:mm） */
  startTime: string;
  /** 是否有复盘报告 */
  hasReview: boolean;
  /** 复盘报告 ID（hasReview 为 true 时存在） */
  reviewId?: number;
}

/** 历史趋势数据点 */
export interface TrendPoint {
  /** 日期/序号标签 */
  label: string;
  /** 技术分 */
  techScore: number;
  /** 协作分 */
  collabScore?: number;
}

/** 个人中心完整数据 */
export interface ProfileData {
  user: UserProfile;
  /** 双维能力雷达（overview.radar，无数据时为零值） */
  radar: DualDimensionRadar;
  /** 历史趋势（最近 N 次训练，时间正序） */
  trend: TrendPoint[];
  /** 训练历史列表（最近 N 条，时间倒序） */
  recentHistory: TrainingHistoryItem[];
  /** 是否可查看人机对照报告（完成了至少 1 次纯人工 + 1 次 AI 协作） */
  canViewComparison: boolean;
  /** 对照报告中最新的两次训练 ID */
  comparisonSessionIds?: {
    manual: number;
    aiCollab: number;
  };
}

// ---------- 后端 DTO（snake_case 与 training-service 对齐） ----------

/** GET /users/me 响应 */
interface ProfileMeDTO {
  id: number;
  username: string;
  account_type: string;
  nickname: string;
  avatar_url: string;
  bio: string;
  email: string;
  phone: string;
  created_at: string;
  collab_level: string;
  collab_group: string;
  avg_score: number | null;
  avg_collab_score: number | null;
  skill_radar: unknown;
  collab_radar: unknown;
}

/** overview.stats 训练统计 */
interface OverviewStatsDTO {
  totalSessions: number;
  finished: number;
  /** 百分比 0-100 */
  successRate: number;
  totalMinutes: number;
  avgTechScore: number | null;
  avgCollabScore: number | null;
}

/** GET /training/sessions/overview 响应 */
interface OverviewDTO {
  stats: OverviewStatsDTO;
  radar: DualDimensionRadar | null;
}

/** 历史列表行（未评分/未结束字段为 null） */
export interface SessionHistoryRowDTO extends TrainingClosureStatus {
  id: number;
  projectName: string;
  defectName: string;
  difficulty: string;
  /** none / assistant */
  aiMode: string;
  techScore: number | null;
  collabScore: number | null;
  success: boolean | null;
  durationSec: number;
  startTime: string | null;
  hasReview: boolean;
  reviewId: number | null;
  createdAt: string;
}

/** GET /training/sessions 分页响应 */
interface SessionListDTO {
  list: SessionHistoryRowDTO[];
  total: number;
  page: number;
  pageSize: number;
}

// ---------- 真实接口 ----------

/** 拉取当前用户资料 */
export async function fetchMyProfile(): Promise<ProfileMeDTO> {
  return http.get<unknown, ProfileMeDTO>("/users/me");
}

/** 基本资料更新入参：未传字段沿用旧值 */
export interface UpdateProfileParams {
  nickname?: string;
  avatarUrl?: string;
  bio?: string;
}

/**
 * 更新当前用户基本资料 PUT /users/me，返回更新后的完整资料。
 * 字段名按后端 snake_case 发送。
 */
export async function updateMyProfile(
  params: UpdateProfileParams,
): Promise<ProfileMeDTO> {
  const body: Record<string, string> = {};
  if (params.nickname !== undefined) body.nickname = params.nickname;
  if (params.avatarUrl !== undefined) body.avatar_url = params.avatarUrl;
  if (params.bio !== undefined) body.bio = params.bio;
  return http.put<unknown, ProfileMeDTO>("/users/me", body);
}

/** 拉取训练概览（统计 + 双维雷达） */
export async function fetchOverview(): Promise<OverviewDTO> {
  return http.get<unknown, OverviewDTO>("/training/sessions/overview");
}

/** 会话列表查询参数 */
export interface SessionListParams {
  page?: number;
  pageSize?: number;
  status?: "finished";
  difficulty?: string;
  aiMode?: string;
  keyword?: string;
  success?: boolean;
}

/** 拉取会话历史（真分页，时间倒序） */
export async function fetchSessions(
  params: SessionListParams = {},
): Promise<SessionListDTO> {
  const query = new URLSearchParams();
  if (params.page) query.set("page", String(params.page));
  if (params.pageSize) query.set("pageSize", String(params.pageSize));
  if (params.status) query.set("status", params.status);
  if (params.difficulty) query.set("difficulty", params.difficulty);
  if (params.aiMode) query.set("aiMode", params.aiMode);
  if (params.keyword) query.set("keyword", params.keyword);
  if (typeof params.success === "boolean") {
    query.set("success", params.success ? "true" : "false");
  }
  const qs = query.toString();
  const path = qs ? `/training/sessions?${qs}` : "/training/sessions";
  return http.get<unknown, SessionListDTO>(path);
}

/** 确认 AI 风险告知（幂等） */
export async function ackRiskNotice(): Promise<void> {
  await http.post<unknown, { acknowledged: boolean }>(
    "/users/me/risk-notice-ack",
    {},
  );
}

/** 趋势取最近 15 次，历史列表展示最近 5 条 */
const TREND_SIZE = 15;
const RECENT_SIZE = 5;

/**
 * 聚合个人中心完整数据：资料 + 概览 + 历史。
 * 三个接口并行请求；人机对照资格从历史模式推导。
 */
export async function fetchProfileData(): Promise<ProfileData> {
  const [me, overview, listRes] = await Promise.all([
    fetchMyProfile(),
    fetchOverview(),
    fetchSessions({ page: 1, pageSize: TREND_SIZE }),
  ]);

  const rows = listRes.list ?? [];
  const history = rows.map(adaptHistoryRow);

  // 后端按时间倒序返回；找最新一场纯人工与 AI 协作
  const latestManual = history.find((h) => h.mode === "manual");
  const latestAiCollab = history.find((h) => h.mode === "ai_collab");
  const canViewComparison = Boolean(latestManual && latestAiCollab);

  return {
    user: adaptUser(me, overview.stats),
    radar: overview.radar ?? emptyRadar(),
    trend: buildTrendFromHistory(history),
    recentHistory: history.slice(0, RECENT_SIZE),
    canViewComparison,
    comparisonSessionIds:
      canViewComparison && latestManual && latestAiCollab
        ? { manual: latestManual.id, aiCollab: latestAiCollab.id }
        : undefined,
  };
}

// ---------- 适配与工具 ----------

/** /users/me + overview.stats → 前端用户信息 */
function adaptUser(me: ProfileMeDTO, stats: OverviewStatsDTO): UserProfile {
  return {
    id: me.id,
    username: me.username,
    nickname: me.nickname || me.username,
    avatar: me.avatar_url || undefined,
    bio: me.bio || undefined,
    email: me.email || undefined,
    phone: me.phone || undefined,
    totalSessions: stats.totalSessions,
    totalMinutes: stats.totalMinutes,
    createdAt: me.created_at,
    collabLevel: me.collab_level || "C1",
  };
}

/** 历史行 DTO → 前端类型：模式映射、空值兜底、时间本地化 */
function adaptHistoryRow(row: SessionHistoryRowDTO): TrainingHistoryItem {
  const item: TrainingHistoryItem = {
    id: row.id,
    projectName: row.projectName,
    defectName: row.defectName,
    difficulty: normalizeDifficulty(row.difficulty),
    mode: row.aiMode === "assistant" ? "ai_collab" : "manual",
    durationSec: row.durationSec,
    startTime: toLocalMinute(row.startTime),
    hasReview: row.hasReview,
    status: row.status,
    brainPhase: row.brainPhase,
    cleanupStatus: row.cleanupStatus,
    reviewLookupFailed: row.reviewLookupFailed,
  };
  if (row.techScore != null) item.techScore = row.techScore;
  if (row.collabScore != null) item.collabScore = row.collabScore;
  if (row.success != null) item.success = row.success;
  if (row.hasReview && row.reviewId != null) item.reviewId = row.reviewId;
  return item;
}

/** 后端难度归一：normal→medium，仅允许 easy/medium/hard */
function normalizeDifficulty(d: string): "easy" | "medium" | "hard" {
  if (d === "easy" || d === "medium" || d === "hard") return d;
  if (d === "normal") return "medium";
  return "medium";
}

/** ISO 时间 → 浏览器当地时间 YYYY-MM-DD HH:mm（解析失败原样返回） */
function toLocalMinute(iso: string | null): string {
  if (!iso) return "";
  return formatIsoMinute(iso);
}

/** 从历史数据生成趋势点（倒序输入 → 时间正序输出） */
export function buildTrendFromHistory(history: TrainingHistoryItem[]): TrendPoint[] {
  return history.slice(0, TREND_SIZE).reverse().map((h, idx) => ({
    label: `第${idx + 1}次`,
    techScore: h.techScore ?? 0,
    collabScore: h.collabScore,
  }));
}

/** 无雷达数据时的零值兜底 */
function emptyRadar(): DualDimensionRadar {
  return {
    currentTech: buildTechRadar({}),
    currentCollab: buildCollabRadar({}),
    techOverall: 0,
    collabOverall: 0,
    hasCollab: false,
    overall: 0,
    level: "C1",
  };
}

/** 格式化难度 */
export function formatDifficulty(d: string): string {
  switch (d) {
    case "easy":
      return "简单";
    case "medium":
      return "中等";
    case "hard":
      return "困难";
    default:
      return d;
  }
}

/** 难度颜色 */
export function difficultyColor(d: string): string {
  switch (d) {
    case "easy":
      return "text-ok-600 bg-ok-50";
    case "medium":
      return "text-warn-600 bg-warn-50";
    case "hard":
      return "text-danger-600 bg-danger-50";
    default:
      return "text-ink-500 bg-ink-50";
  }
}

/** 格式化模式 */
export function formatMode(m: string): string {
  return m === "manual" ? "纯人工" : "AI 协作";
}

/** 模式图标 */
export function modeIcon(m: string): string {
  return m === "manual" ? "🧑‍💻" : "🤖";
}
