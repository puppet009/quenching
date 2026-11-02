// 项目相关接口（对接 content-service）。
// 类型后续从 OpenAPI 自动生成，这里先手写。
import { http } from "./http";

/** 项目业务领域 */
export type ProjectDomain = "ecommerce" | "im" | "saas" | "iot" | "game";

/** 项目状态 */
export type ProjectStatus = "online" | "offline" | "draft";

/** 难度等级 */
export type DifficultyLevel = "easy" | "medium" | "hard";

/** 项目详情 */
export interface Project {
  id: number;
  name: string;
  domain: ProjectDomain;
  description: string;
  tech_stack: string[];
  architecture_image: string;
  deploy_config: Record<string, unknown> | null;
  difficulty: DifficultyLevel;
  status: ProjectStatus;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

/** 项目列表响应 */
export interface ProjectListResp {
  total: number;
  list: Project[];
}

/** 列表查询参数 */
export interface ListProjectsParams {
  domain?: string;
  status?: string;
  difficulty?: string;
  keyword?: string;
  page?: number;
  size?: number;
}

/** 创建项目请求 */
export interface CreateProjectParams {
  name: string;
  domain: string;
  description?: string;
  tech_stack?: string[];
  architecture_image?: string;
  deploy_config?: Record<string, unknown>;
  difficulty?: DifficultyLevel;
  sort_order?: number;
}

/** 更新项目请求 */
export interface UpdateProjectParams {
  name?: string;
  domain?: string;
  description?: string;
  tech_stack?: string[];
  architecture_image?: string;
  deploy_config?: Record<string, unknown>;
  difficulty?: DifficultyLevel;
  status?: ProjectStatus;
  sort_order?: number;
}

/** 服务类型 */
export type ServiceType = "app" | "database" | "cache" | "mq" | "gateway";

/** 项目服务（微服务） */
export interface ProjectServiceItem {
  id: number;
  project_id: number;
  name: string;
  display_name: string;
  service_type: ServiceType;
  tech_stack: string;
  image: string;
  ports: unknown[];
  dependencies: number[];
  sort_order: number;
  created_at: string;
  updated_at: string;
}

/** 项目部署配置 */
export interface ProjectDeployConfig {
  id: number;
  project_id: number;
  k8s_namespace_template: string;
  helm_chart: string;
  init_script: string;
  helm_values: Record<string, unknown>;
  ingress_config: Record<string, unknown>;
  storage_config: Record<string, unknown>;
  health_check: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

/** 获取项目列表 */
export function listProjects(params?: ListProjectsParams) {
  return http.get<unknown, ProjectListResp>("/projects", { params });
}

/** 获取项目详情 */
export function getProject(id: number) {
  return http.get<unknown, Project>(`/projects/${id}`);
}

/** 创建项目（管理后台） */
export function createProject(data: CreateProjectParams) {
  return http.post<unknown, Project>("/admin/projects", data);
}

/** 更新项目（管理后台） */
export function updateProject(id: number, data: UpdateProjectParams) {
  return http.put<unknown, null>(`/admin/projects/${id}`, data);
}

/** 删除项目（管理后台） */
export function deleteProject(id: number) {
  return http.delete<unknown, null>(`/admin/projects/${id}`);
}

/** 更新项目状态（上下线） */
export function updateProjectStatus(id: number, status: ProjectStatus) {
  return http.patch<unknown, null>(`/admin/projects/${id}/status`, { status });
}

/** 获取项目下的服务列表 */
export function listProjectServices(projectId: number) {
  return http.get<unknown, ProjectServiceItem[]>(`/projects/${projectId}/services`);
}

/** 获取项目部署配置 */
export function getProjectDeployConfig(projectId: number) {
  return http.get<unknown, ProjectDeployConfig>(`/projects/${projectId}/deploy-config`);
}

// ===== 前端辅助函数 =====

/** 领域显示名映射 */
export const DOMAIN_LABELS: Record<ProjectDomain, string> = {
  ecommerce: "电商系统",
  im: "即时通讯",
  saas: "SaaS 平台",
  iot: "物联网",
  game: "游戏服务端",
};

/** 领域图标（用 emoji 简化表示，MVP 阶段够用） */
export const DOMAIN_ICONS: Record<ProjectDomain, string> = {
  ecommerce: "🛒",
  im: "💬",
  saas: "📊",
  iot: "📡",
  game: "🎮",
};

/** 难度显示名 */
export const DIFFICULTY_LABELS: Record<DifficultyLevel, string> = {
  easy: "简单",
  medium: "中等",
  hard: "困难",
};

/** 难度颜色（管理后台用） */
export const DIFFICULTY_COLORS: Record<DifficultyLevel, string> = {
  easy: "text-ok-600 bg-ok-50",
  medium: "text-warn-600 bg-warn-50",
  hard: "text-danger-600 bg-danger-50",
};

/** 状态显示名 */
export const STATUS_LABELS: Record<ProjectStatus, string> = {
  online: "已上线",
  offline: "已下线",
  draft: "草稿",
};

/** 状态颜色 */
export const STATUS_COLORS: Record<ProjectStatus, string> = {
  online: "text-ok-600 bg-ok-50",
  offline: "text-ink-500 bg-ink-100",
  draft: "text-warn-600 bg-warn-50",
};

/** 服务类型显示名 */
export const SERVICE_TYPE_LABELS: Record<ServiceType, string> = {
  app: "应用服务",
  database: "数据库",
  cache: "缓存",
  mq: "消息队列",
  gateway: "网关",
};

/** 服务类型图标 */
export const SERVICE_TYPE_ICONS: Record<ServiceType, string> = {
  app: "⚙️",
  database: "🗄️",
  cache: "⚡",
  mq: "📨",
  gateway: "🚪",
};

/** 服务类型颜色 */
export const SERVICE_TYPE_COLORS: Record<ServiceType, string> = {
  app: "text-brand-500 bg-brand-50 border-brand-100",
  database: "text-ai bg-ai/10 border-ai/20",
  cache: "text-warn bg-warn/10 border-warn/20",
  mq: "text-ok bg-ok/10 border-ok/20",
  gateway: "text-ink-500 bg-ink-100 border-ink-200",
};

/**
 * 根据技术栈数量推断难度（MVP 临时方案，后端加字段后替换）
 */
export function inferDifficulty(techStack: string[]): DifficultyLevel {
  const len = techStack?.length ?? 0;
  if (len <= 2) return "easy";
  if (len <= 4) return "medium";
  return "hard";
}

/** 估算训练时长（基于难度） */
export function estimateDuration(difficulty: DifficultyLevel): string {
  switch (difficulty) {
    case "easy":
      return "约 30 分钟";
    case "medium":
      return "约 60 分钟";
    case "hard":
      return "约 90 分钟";
  }
}

/** 训练收获列表（MVP 阶段基于难度生成，后续从后端配置读取） */
export function getTrainingOutcomes(difficulty: DifficultyLevel): string[] {
  const base = [
    "掌握真实生产环境故障排查思路",
    "学会使用系统监控和日志定位问题",
    "提升 AI 辅助编程时的独立判断力",
  ];
  if (difficulty === "medium" || difficulty === "hard") {
    base.push("理解微服务架构下的故障传导机制");
  }
  if (difficulty === "hard") {
    base.push("掌握复杂故障场景下的系统性分析方法");
    base.push("提升高压环境下的决策与时间管理能力");
  }
  return base;
}
