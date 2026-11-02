// 实训会话生命周期接口（第五片）
// 创建 / 开始 / 结束 / 详情 / 操作批量上报，对应 training-service：
// POST /training/sessions、POST /sessions/:id/start、POST /sessions/:id/finish、
// GET  /training/sessions/:id、POST /sessions/:id/operations。

import { http } from "@quenching/shared";

/** POST /training/sessions 请求体 */
export interface CreateSessionPayload {
  /** 数据库登记项目 ID；文件模板模式传 0 */
  project_id: number;
  /** 文件模板 slug；与 project_id 至少传一个 */
  template_slug?: string;
  /**
   * 起始难度 easy/normal/hard。
   * 留空由系统按用户能力判定（班次内实时升降），实训入口不再让用户选择。
   */
  difficulty?: string;
}

/** 创建会话响应 */
export interface CreatedSession {
  id: number;
  status: string;
  /** none / assistant */
  aiMode: string;
  /** 控制组 A/B，未分组为空 */
  controlGroup: string;
}

/** 会话详情（训练页 header 数据源） */
export interface SessionDetail {
  id: number;
  status: string;
  aiMode: string;
  controlGroup: string;
  difficulty: string;
  projectId: number;
  projectName: string;
  /** 盲盒：会话处于活跃状态时后端固定回传空串，仅终态后带出故障名 */
  defectName: string;
  startTime: string | null;
  createdAt: string;
}

/** 单条操作上报（snake_case 与后端 OperationInput 对齐） */
export interface OperationInput {
  operation_type: string;
  operation_category: string;
  target: string;
  detail?: unknown;
  result?: string;
  penalty?: number;
  virtual_timestamp_sec?: number;
}

/** 结束会话结果 */
export interface FinishedSession {
  id: number;
  status: string;
  /** completed / aborted / failed */
  outcome: string;
}

/** 创建实训会话 */
export async function createSession(
  payload: CreateSessionPayload,
): Promise<CreatedSession> {
  return http.post<unknown, CreatedSession>("/training/sessions", payload);
}

/** 开始会话（进入训练页调用；已开始时后端幂等返回 resolved=false） */
export async function startSession(sessionId: number): Promise<void> {
  await http.post<unknown, { resolved: boolean }>(
    `/training/sessions/${sessionId}/start`,
    {},
  );
}

/**
 * 结束会话。
 * @param outcome completed（修复完成）/ aborted（主动离开）/ failed
 */
export async function finishSession(
  sessionId: number,
  outcome: "completed" | "aborted" | "failed",
): Promise<FinishedSession> {
  return http.post<unknown, FinishedSession>(
    `/training/sessions/${sessionId}/finish`,
    { outcome },
  );
}

/** 拉取会话详情 */
export async function fetchSessionDetail(sessionId: number): Promise<SessionDetail> {
  return http.get<unknown, SessionDetail>(`/training/sessions/${sessionId}`);
}

/** 批量上报操作，返回实际接收条数 */
export async function reportOperations(
  sessionId: number,
  operations: OperationInput[],
): Promise<number> {
  if (operations.length === 0) return 0;
  const res = await http.post<unknown, { accepted: number }>(
    `/training/sessions/${sessionId}/operations`,
    { operations },
  );
  return res.accepted;
}

/** Contract: docs/api/brain-status.openapi.yaml */
export interface BrainSessionStatus {
  phase: string;
  current_challenge: number;
  status: string;
  reason: string;
  cleanup_status: string;
  ability_observed: boolean;
}
export function fetchBrainStatus(sessionId: number, signal?: AbortSignal): Promise<BrainSessionStatus> {
  return http.get<unknown, BrainSessionStatus>(`/training/sessions/${sessionId}/brain`, { signal });
}
