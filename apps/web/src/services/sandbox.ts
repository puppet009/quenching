// 实训沙箱接口（第五片）
// POST /sandboxes 创建；GET /sandboxes/:id/progress 轮询启动进度。
// 用户拍板：沙箱必须 ready 才进入训练页，未就绪一律不跳转。

import { http } from "@quenching/shared";
import { tokenStore } from "@quenching/shared";

/** POST /sandboxes 请求体 */
export interface CreateSandboxPayload {
  session_id: number;
  project_id: number;
  template_slug?: string;
}

/** 沙箱记录（与 sandbox-service model.Sandbox 对齐） */
export interface SandboxRecord {
  id: number;
  session_id: number;
  user_id: number;
  project_id: number;
  namespace: string;
  status: string;
  access_url?: string;
  created_at: string;
}

/** 沙箱启动进度（与 service.ProgressStatus 对齐） */
export interface SandboxProgress {
  session_id: number;
  /** queued / creating / deploying / ready / failed */
  status: string;
  stage: string;
  stage_name: string;
  /** 0-100 */
  percent: number;
  message: string;
  elapsed_sec: number;
  estimated_sec: number;
  ready_count: number;
  total_count: number;
  error?: string;
  started_at: string;
}

/** Backend synchronously waits up to five minutes for Helm provisioning. */
const CREATE_SANDBOX_TIMEOUT_MS = 6 * 60 * 1000;

/** 创建沙箱 */
export async function createSandbox(
  payload: CreateSandboxPayload,
): Promise<SandboxRecord> {
  return http.post<unknown, SandboxRecord>("/sandboxes", payload, {
    timeout: CREATE_SANDBOX_TIMEOUT_MS,
  });
}

/** Pod 内可切换容器（与 sandbox-service service.ContainerTarget 对齐） */
export interface ContainerTarget {
  name: string;
  /** 终端起始工作目录；空值表示使用容器默认目录。 */
  working_dir?: string;
  /** 运行产物目录，与可编辑源码目录可能不同。 */
  deployment_dir?: string;
  /** 服务配置目录。 */
  config_dir?: string;
}

/** 桌面服务器目标（与 sandbox-service service.ServerTarget 对齐） */
export interface ServerTarget {
  /** Pod 名，终端连接的 target 参数 */
  name: string;
  /** 去掉哈希后缀的可读名，用于桌面展示 */
  displayName?: string;
  /** Pod 阶段 */
  phase: string;
  /** 是否可连 */
  ready: boolean;
  /** 该 Pod 内可 exec 的容器 */
  containers: ContainerTarget[];
}

/** 列出沙箱内全部服务器目标 */
export async function listTargets(sessionId: number): Promise<ServerTarget[]> {
  return http.get<unknown, ServerTarget[]>(`/sandboxes/${sessionId}/targets`);
}

/**
 * 构造真实终端 WebSocket 地址。
 * 走网关 /api/v1（Vite 本地代理已开启 ws），token 作为 query 传递
 * （浏览器原生 WS 无法设置 Authorization 头，网关仅对升级请求放行 query token）。
 */
export function buildExecWSURL(
  sessionId: number,
  target: string,
  container?: string,
): string {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  const params = new URLSearchParams({ target });
  const token = tokenStore.get();
  if (token) params.set("token", token);
  if (container) params.set("container", container);
  return `${proto}//${window.location.host}/api/v1/sandboxes/${sessionId}/exec?${params.toString()}`;
}

/** 查询沙箱进度（路径参数用 session_id，tracker 按会话查） */
export async function fetchSandboxProgress(
  sessionId: number,
): Promise<SandboxProgress> {
  return http.get<unknown, SandboxProgress>(`/sandboxes/${sessionId}/progress`);
}

/** 轮询间隔与超时 */
const POLL_INTERVAL_MS = 2000;
/** 最长等待 10 分钟，超时按失败处理 */
const POLL_TIMEOUT_MS = 10 * 60 * 1000;

/**
 * 轮询直到沙箱 ready 才 resolve；failed/超时 reject。
 * @param onProgress 每次轮询回调，用于更新进度条
 */
export function waitUntilReady(
  sessionId: number,
  onProgress?: (p: SandboxProgress) => void,
): Promise<SandboxProgress> {
  return new Promise((resolve, reject) => {
    const deadline = Date.now() + POLL_TIMEOUT_MS;

    const tick = async () => {
      try {
        const p = await fetchSandboxProgress(sessionId);
        onProgress?.(p);

        if (p.status === "ready") {
          resolve(p);
          return;
        }
        if (p.status === "released" || p.status === "releasing") { reject(new Error("本次实训已结束，环境不会继续启动")); return; }
        if (p.status === "failed") {
          reject(new Error(p.error || "沙箱启动失败，请重试"));
          return;
        }
        if (Date.now() >= deadline) {
          reject(new Error(p.status === "queued" ? "仍在等待可用资源，本次申请已保留，请在实训记录中查看" : "沙箱启动超时，请查看本次实训进度"));
          return;
        }
        window.setTimeout(tick, POLL_INTERVAL_MS);
      } catch (err) {
        // 单次轮询网络抖动不立即失败，未超时则继续
        if (Date.now() >= deadline) {
          reject(err instanceof Error ? err : new Error("沙箱进度查询失败"));
          return;
        }
        window.setTimeout(tick, POLL_INTERVAL_MS);
      }
    };

    void tick();
  });
}
