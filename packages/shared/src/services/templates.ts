// 官方预置实训模板只读接口（对接 content-service GET /api/v1/templates）。
// 列表数据由后端扫描 templates/<slug>/template.yaml 及可选 catalog.yaml 得到，不经过数据库；
// 类型与 go-common/templates.Manifest 保持一致，改字段时两边同步。
import { http } from "./http";

/** 模板难度（清单可选字段，缺省为空串） */
export type TemplateDifficulty = "easy" | "medium" | "hard" | "";

/** 模板内服务角色：business 纳入恢复评分，middleware 仅为基础设施 */
export type TemplateServiceRole = "business" | "middleware";

/** 模板内服务声明 */
export interface TemplateService {
  name: string;
  display_name?: string;
  description?: string;
  /** K8s Deployment/StatefulSet 名，缺省与 name 相同 */
  workload: string;
  port: number;
  role: TemplateServiceRole;
}

/** 随模板构建的私有镜像声明；全部走公共镜像的模板该字段为 null */
export interface TemplateImage {
  /** 与 services[].name 对应 */
  name: string;
  context: string;
  dockerfile: string;
}

/** 单个沙箱实例的总资源预算（可选，缺省为 null） */
export interface TemplateResources {
  cpu: string;
  memory: string;
}

/** 模板自带的单张展示封面；由内容服务按登记图片提供，不依赖外部图床。 */
export interface TemplateCover {
  url: string;
  alt: string;
  width: number;
  height: number;
}

/** 模板清单及可选的用户展示资料（与后端 Manifest 对应）。 */
export interface Template {
  apiVersion: string;
  /** 模板唯一标识，同时兼作沙箱内 Helm release 名；创建沙箱时作为 template_slug 上送 */
  slug: string;
  name: string;
  domain: string;
  description: string;
  business_scenarios?: string[];
  learning_objectives?: string[];
  overview?: string[];
  audience?: string[];
  cover?: TemplateCover;
  difficulty: TemplateDifficulty;
  tech_stack: string[] | null;
  /** 模板目录内的 Chart 相对目录（部署用，前端展示不依赖） */
  chart: string;
  sort_order: number;
  /** 缺省启用；列表接口只返回已启用模板，null 按启用处理 */
  enabled: boolean | null;
  services: TemplateService[];
  images: TemplateImage[] | null;
  resources: TemplateResources | null;
}

/** 模板是否启用：enabled 缺省（null/undefined）视为启用。 */
export function isTemplateEnabled(template: Template): boolean {
  return template.enabled == null || template.enabled;
}

/** 获取已启用的官方模板列表（后端按 sort_order 升序、再按 slug 升序返回）。 */
export function listTemplates() {
  return http.get<unknown, Template[]>("/templates");
}
