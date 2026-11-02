// 项目故障只读接口（第五片）
// GET /projects/:id/defects —— content-service 用户面故障概览，
// 仅含实训前需要了解的非敏感字段（根因/注入配置不下发）。

import { http } from "@quenching/shared";

/** 项目故障卡片（与 content-service dto.ProjectDefectItem 对齐） */
export interface ProjectDefect {
  id: number;
  project_id: number;
  code: string;
  name: string;
  defect_type: string;
  category: string;
  /** 难度: easy/normal/hard */
  difficulty: string;
  description: string;
  target_service_id: number | null;
  score_weight: number;
  /** 预计修复时长（分钟） */
  estimated_fix_time: number;
  sort_order: number;
}

/**
 * 拉取项目下的启用故障列表（按 sort_order,id 排序）。
 * 失败时由调用方降级为空态，不阻塞项目详情展示。
 */
export async function fetchProjectDefects(projectId: number): Promise<ProjectDefect[]> {
  return http.get<unknown, ProjectDefect[]>(`/projects/${projectId}/defects`);
}
