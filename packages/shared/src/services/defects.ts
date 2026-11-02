// 故障剧本服务（T2-25）
// 对应 fault-engine 的 defects 接口

/** 故障类型 */
export type DefectType = "code" | "config" | "architecture" | "resource";

/** 故障分类 */
export type DefectCategory =
  | "database"
  | "cache"
  | "mq"
  | "network"
  | "cpu"
  | "memory"
  | "disk"
  | "threadpool";

/** 难度等级 */
export type DefectDifficulty = "easy" | "medium" | "hard";

/** 故障剧本 */
export interface Defect {
  id: number;
  project_id: number;
  code: string;
  name: string;
  defect_type: DefectType;
  category: DefectCategory;
  difficulty: DefectDifficulty;
  description: string;
  root_cause: string;
  target_service_id: number;
  /** 触发条件配置 */
  trigger_config: Record<string, unknown>;
  /** 表现特征配置（指标/日志/告警） */
  manifestation_config: Record<string, unknown>;
  /** 修复验证配置 */
  fix_config: Record<string, unknown>;
  /** 放大器配置（ChaosBlade 注入） */
  amplifier_config: Record<string, unknown>;
  /** 弹性冻结规则 */
  freeze_rules: Record<string, unknown>;
  score_weight: number;
  estimated_fix_time: number; // 分钟
  is_active: boolean;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

/** 剧本列表查询参数 */
export interface ListDefectsParams {
  project_id: number;
  difficulty?: DefectDifficulty;
  category?: string;
  is_active?: boolean;
  page?: number;
  size?: number;
}

/** 剧本列表响应 */
export interface DefectListResp {
  total: number;
  list: Defect[];
}

/* ===== 显示配置 ===== */

/** 故障类型显示名 */
export const DEFECT_TYPE_LABELS: Record<DefectType, string> = {
  code: "代码缺陷",
  config: "配置错误",
  architecture: "架构问题",
  resource: "资源耗尽",
};

/** 故障分类显示名 */
export const DEFECT_CATEGORY_LABELS: Record<DefectCategory, string> = {
  database: "数据库",
  cache: "缓存",
  mq: "消息队列",
  network: "网络",
  cpu: "CPU",
  memory: "内存",
  disk: "磁盘",
  threadpool: "线程池",
};

/** 故障分类图标 */
export const DEFECT_CATEGORY_ICONS: Record<DefectCategory, string> = {
  database: "🗄️",
  cache: "⚡",
  mq: "📨",
  network: "🌐",
  cpu: "💻",
  memory: "🧠",
  disk: "💾",
  threadpool: "🧵",
};

/** 难度显示名 */
export const DEFECT_DIFFICULTY_LABELS: Record<DefectDifficulty, string> = {
  easy: "简单",
  medium: "中等",
  hard: "困难",
};

/** 难度颜色样式 */
export const DEFECT_DIFFICULTY_COLORS: Record<DefectDifficulty, string> = {
  easy: "text-ok-600 bg-ok-50 border-ok-200",
  medium: "text-warn-600 bg-warn-50 border-warn-200",
  hard: "text-danger-600 bg-danger-50 border-danger-200",
};

/* ===== API 封装 ===== */

const API_BASE = "/api/v1/defects";

/** 获取项目下的故障剧本列表 */
export async function listDefects(params: ListDefectsParams): Promise<DefectListResp> {
  const search = new URLSearchParams();
  search.set("project_id", String(params.project_id));
  if (params.difficulty) search.set("difficulty", params.difficulty);
  if (params.category) search.set("category", params.category);
  if (params.is_active !== undefined) search.set("is_active", String(params.is_active));
  if (params.page) search.set("page", String(params.page));
  if (params.size) search.set("size", String(params.size));

  const res = await fetch(`${API_BASE}?${search.toString()}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取故障列表失败");
  return json.data;
}

/** 获取故障剧本详情 */
export async function getDefect(id: number): Promise<Defect> {
  const res = await fetch(`${API_BASE}/${id}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取故障详情失败");
  return json.data;
}

/* ===== Mock 数据 ===== */

/** 模拟故障剧本数据（简单难度 2 个） */
export const MOCK_EASY_DEFECTS: Defect[] = [
  {
    id: 1,
    project_id: 1,
    code: "cpu-stress-order-01",
    name: "订单服务CPU满载",
    defect_type: "resource",
    category: "cpu",
    difficulty: "easy",
    description: "订单服务CPU使用率持续100%，接口响应变慢",
    root_cause:
      "order-service 内有异常进程占用大量CPU资源，导致业务请求无法及时处理，表现为接口延迟飙升、QPS下降",
    target_service_id: 3,
    trigger_config: { type: "delay", delay_seconds: 30 },
    manifestation_config: {
      metrics: [
        { name: "cpu_usage", service: "order-service", op: "gt", value: 90 },
      ],
      alerts: [
        { level: "critical", message: "订单服务CPU使用率超过90%" },
      ],
    },
    fix_config: {
      steps: [
        { step: 1, description: "通过监控确认CPU使用率异常" },
        { step: 2, description: "使用top/ps定位高CPU进程" },
        { step: 3, description: "终止异常进程或扩容资源" },
      ],
      verify: "CPU使用率回落至正常水平，接口延迟恢复",
    },
    amplifier_config: {
      code: "cpu-stress-order-01",
      kind: "cpu_load",
      title: "订单服务CPU满载",
      target: { pod: "order-service" },
      spec: { kind: "cpu_load", cpu_percent: 100 },
      trigger: { type: "delay", delay_seconds: 30 },
      recovery: [
        { metric: "cpu_usage", service: "order-service", op: "lte", value: 70 },
      ],
      auto_revoke: true,
    },
    freeze_rules: { max_cpu_percent: 95, stable_minutes: 2 },
    score_weight: 1.0,
    estimated_fix_time: 10,
    is_active: true,
    sort_order: 1,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  },
  {
    id: 2,
    project_id: 1,
    code: "disk-fill-product-01",
    name: "商品服务磁盘打满",
    defect_type: "resource",
    category: "disk",
    difficulty: "easy",
    description: "商品服务磁盘被日志文件占满，服务无法正常写入",
    root_cause:
      "product-service 日志文件未配置轮转策略，日积月累占满磁盘空间，导致服务无法写入新日志和数据，最终服务异常",
    target_service_id: 2,
    trigger_config: { type: "delay", delay_seconds: 60 },
    manifestation_config: {
      metrics: [
        { name: "disk_usage", service: "product-service", op: "gt", value: 90 },
      ],
      alerts: [
        { level: "warning", message: "商品服务磁盘使用率超过90%" },
      ],
    },
    fix_config: {
      steps: [
        { step: 1, description: "通过监控确认磁盘使用率异常" },
        { step: 2, description: "使用df/du定位大文件目录" },
        { step: 3, description: "清理过期日志文件并配置轮转" },
      ],
      verify: "磁盘使用率回落至正常水平，服务恢复正常写入",
    },
    amplifier_config: {
      code: "disk-fill-product-01",
      kind: "disk_fill",
      title: "商品服务磁盘打满",
      target: { pod: "product-service" },
      spec: { kind: "disk_fill", path: "/data/logs", size_percent: 95 },
      trigger: { type: "delay", delay_seconds: 60 },
      recovery: [
        { metric: "disk_usage", service: "product-service", op: "lte", value: 80 },
      ],
      auto_revoke: true,
    },
    freeze_rules: { max_disk_percent: 90, stable_minutes: 2 },
    score_weight: 1.0,
    estimated_fix_time: 15,
    is_active: true,
    sort_order: 2,
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  },
];

/** 获取所有模拟故障剧本（包括简单+中等，供前端 demo 使用） */
export function getMockDefects(projectId = 1): Defect[] {
  return MOCK_EASY_DEFECTS.filter((d) => d.project_id === projectId);
}

/* ===== 工具函数 ===== */

/** 格式化预计修复时间 */
export function formatFixTime(minutes: number): string {
  if (minutes < 60) return `${minutes} 分钟`;
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return mins > 0 ? `${hours} 小时 ${mins} 分` : `${hours} 小时`;
}
