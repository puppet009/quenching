// 配置查看服务（T2-24）
// 对应 training-service 的配置查询接口（只读查看沙箱内服务配置）

/** 配置项类型 */
export type ConfigType = "env" | "file" | "deployment" | "resource";

/** 单个配置项 */
export interface ConfigItem {
  /** 配置键名 */
  key: string;
  /** 配置值 */
  value: string;
  /** 配置描述（可选） */
  description?: string;
  /** 是否敏感（脱敏显示） */
  sensitive?: boolean;
}

/** 配置文件 */
export interface ConfigFile {
  /** 文件 ID */
  id: string;
  /** 文件名 */
  name: string;
  /** 文件路径 */
  path: string;
  /** 文件类型 */
  type: ConfigType;
  /** 文件大小（字节） */
  size: number;
  /** 修改时间（毫秒时间戳） */
  modified_at: number;
  /** 配置项列表（键值对类配置） */
  items?: ConfigItem[];
  /** 原始内容（文本类配置） */
  content?: string;
}

/** 服务部署信息 */
export interface ServiceDeployInfo {
  /** 服务名 */
  service: string;
  /** 镜像地址 */
  image: string;
  /** 镜像标签 */
  image_tag: string;
  /** 副本数 */
  replicas: number;
  /** 可用副本数 */
  ready_replicas: number;
  /** CPU 请求 */
  cpu_request: string;
  /** 内存请求 */
  memory_request: string;
  /** CPU 限制 */
  cpu_limit: string;
  /** 内存限制 */
  memory_limit: string;
  /** 端口列表 */
  ports: number[];
  /** 命名空间 */
  namespace: string;
  /** Pod 列表 */
  pods: string[];
  /** 启动时间（毫秒时间戳） */
  started_at: number;
  /** 运行时长（毫秒） */
  uptime: number;
}

/** 配置类型显示配置 */
export const CONFIG_TYPE_CONFIG: Record<ConfigType, {
  label: string;
  icon: string;
  color: string;
}> = {
  env: {
    label: "环境变量",
    icon: "🌐",
    color: "text-brand-600",
  },
  file: {
    label: "配置文件",
    icon: "📄",
    color: "text-ink-600",
  },
  deployment: {
    label: "部署信息",
    icon: "🚀",
    color: "text-ok-600",
  },
  resource: {
    label: "资源配置",
    icon: "⚙️",
    color: "text-warn-600",
  },
};

/* ===== API 封装 ===== */

const API_BASE = "/api/v1/configs";

/** 获取服务配置文件列表 */
export async function listConfigFiles(service: string): Promise<ConfigFile[]> {
  const res = await fetch(`${API_BASE}/files?service=${encodeURIComponent(service)}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取配置列表失败");
  return json.data;
}

/** 获取配置文件详情 */
export async function getConfigFile(service: string, fileId: string): Promise<ConfigFile> {
  const res = await fetch(
    `${API_BASE}/file?service=${encodeURIComponent(service)}&file_id=${encodeURIComponent(fileId)}`,
  );
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取配置详情失败");
  return json.data;
}

/** 获取服务部署信息 */
export async function getServiceDeployInfo(service: string): Promise<ServiceDeployInfo> {
  const res = await fetch(`${API_BASE}/deploy?service=${encodeURIComponent(service)}`);
  const json = await res.json();
  if (json.code !== 0) throw new Error(json.msg || "获取部署信息失败");
  return json.data;
}

/* ===== Mock 数据生成 ===== */

const ENV_TEMPLATES: Array<{ key: string; description: string; sensitive?: boolean }> = [
  { key: "DB_HOST", description: "数据库地址" },
  { key: "DB_PORT", description: "数据库端口" },
  { key: "DB_NAME", description: "数据库名" },
  { key: "DB_USER", description: "数据库用户" },
  { key: "DB_PASSWORD", description: "数据库密码", sensitive: true },
  { key: "REDIS_HOST", description: "Redis 地址" },
  { key: "REDIS_PORT", description: "Redis 端口" },
  { key: "REDIS_PASSWORD", description: "Redis 密码", sensitive: true },
  { key: "SERVER_PORT", description: "服务端口" },
  { key: "LOG_LEVEL", description: "日志级别" },
  { key: "MAX_CONNECTIONS", description: "最大连接数" },
  { key: "TIMEOUT_MS", description: "请求超时时间(ms)" },
  { key: "ENABLE_CACHE", description: "是否启用缓存" },
  { key: "JWT_SECRET", description: "JWT 密钥", sensitive: true },
  { key: "RATE_LIMIT", description: "限流阈值(QPS)" },
];

const FILE_TEMPLATES = [
  { name: "application.yml", path: "/config/application.yml", type: "file" as const },
  { name: "config.json", path: "/app/config.json", type: "file" as const },
  { name: "startup.sh", path: "/app/startup.sh", type: "file" as const },
];

/** 生成模拟环境变量配置 */
export function generateMockEnvConfig(service: string): ConfigItem[] {
  const values: Record<string, string> = {
    DB_HOST: `${service}-db.internal`,
    DB_PORT: "5432",
    DB_NAME: `${service.replace("-", "_")}_db`,
    DB_USER: "app_user",
    DB_PASSWORD: "********",
    REDIS_HOST: "redis-cache.internal",
    REDIS_PORT: "6379",
    REDIS_PASSWORD: "********",
    SERVER_PORT: "8080",
    LOG_LEVEL: "info",
    MAX_CONNECTIONS: "100",
    TIMEOUT_MS: "5000",
    ENABLE_CACHE: "true",
    JWT_SECRET: "********",
    RATE_LIMIT: "200",
  };

  return ENV_TEMPLATES.map((t) => ({
    key: t.key,
    value: values[t.key] || "default",
    description: t.description,
    sensitive: t.sensitive,
  }));
}

/** 生成模拟配置文件列表 */
export function generateMockConfigFiles(service: string): ConfigFile[] {
  const files: ConfigFile[] = [];

  // 环境变量
  files.push({
    id: `${service}-env`,
    name: "环境变量",
    path: "/etc/environment",
    type: "env",
    size: 1024 + Math.floor(Math.random() * 512),
    modified_at: Date.now() - Math.random() * 7 * 24 * 60 * 60 * 1000,
    items: generateMockEnvConfig(service),
  });

  // 配置文件
  FILE_TEMPLATES.forEach((tpl, idx) => {
    const content = generateMockFileContent(tpl.name, service);
    files.push({
      id: `${service}-file-${idx}`,
      name: tpl.name,
      path: tpl.path,
      type: tpl.type,
      size: content.length,
      modified_at: Date.now() - Math.random() * 3 * 24 * 60 * 60 * 1000,
      content,
    });
  });

  return files;
}

/** 生成模拟部署信息 */
export function generateMockDeployInfo(service: string): ServiceDeployInfo {
  const replicas = 2 + Math.floor(Math.random() * 2);
  const readyReplicas = replicas - (Math.random() > 0.8 ? 1 : 0);
  const startedAt = Date.now() - Math.random() * 3 * 24 * 60 * 60 * 1000;

  return {
    service,
    image: `registry.internal/${service}`,
    image_tag: "v1.2.3",
    replicas,
    ready_replicas: readyReplicas,
    cpu_request: "100m",
    memory_request: "256Mi",
    cpu_limit: "500m",
    memory_limit: "512Mi",
    ports: [8080, 9090],
    namespace: "sandbox-12345",
    pods: Array.from({ length: replicas }, (_, i) => `${service}-${Math.random().toString(36).slice(2, 8)}-${i + 1}`),
    started_at: startedAt,
    uptime: Date.now() - startedAt,
  };
}

/** 生成模拟配置文件内容 */
function generateMockFileContent(fileName: string, service: string): string {
  if (fileName.endsWith(".yml") || fileName.endsWith(".yaml")) {
    return `server:
  port: 8080
  servlet:
    context-path: /api

spring:
  application:
    name: ${service}
  datasource:
    url: jdbc:postgresql://${service}-db.internal:5432/${service.replace("-", "_")}_db
    username: app_user
    password: \${DB_PASSWORD}
    hikari:
      maximum-pool-size: 20
      minimum-idle: 5
  redis:
    host: redis-cache.internal
    port: 6379
    timeout: 3000ms

logging:
  level:
    root: INFO
    com.example: DEBUG

management:
  endpoints:
    web:
      exposure:
        include: health,info,metrics
  endpoint:
    health:
      show-details: always`;
  }

  if (fileName.endsWith(".json")) {
    return JSON.stringify(
      {
        serviceName: service,
        version: "1.2.3",
        features: {
          enableCache: true,
          enableMetrics: true,
          enableTracing: false,
        },
        rateLimit: {
          default: 200,
          perIp: 100,
        },
        retry: {
          maxAttempts: 3,
          backoff: 1000,
        },
      },
      null,
      2,
    );
  }

  if (fileName.endsWith(".sh")) {
    return `#!/bin/bash
set -e

echo "Starting ${service}..."

# Load environment
if [ -f /etc/environment ]; then
    source /etc/environment
fi

# Check dependencies
echo "Checking database connection..."
sleep 2

# Start service
exec java -jar /app/${service}.jar \
    --server.port=\${SERVER_PORT:-8080} \
    --spring.profiles.active=\${SPRING_PROFILE:-default}`;
  }

  return `# ${service} configuration file
# Auto-generated
key=value
setting=default`;
  }

/* ===== 工具函数 ===== */

/** 格式化文件大小 */
export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

/** 格式化运行时长 */
export function formatUptime(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = seconds % 60;

  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${minutes}分钟`;
  if (minutes > 0) return `${minutes}分 ${secs}秒`;
  return `${secs}秒`;
}
