// 文件管理器前端 API（文件管理器第二批，对标腾讯云 OrcaTerm）。
//
// 统一走网关 /api/v1/sandboxes/:id/files/*，pod/container/path 经 query 传递；
// 领域类型与后端 service.FileEntry 对齐。读/写/改名/权限/复制/移动/删除为
// POST JSON；上传为 multipart；下载经浏览器直接拉取（带 Authorization 头时
// 使用 blob 触发，确保鉴权）。
import { http, tokenStore } from "@quenching/shared";

/** 文件列表一项（与后端 service.FileEntry 对齐）。 */
export interface FileEntry {
  name: string;
  path: string;
  is_dir: boolean;
  mode: string; // 数字权限，如 0755
  permissions: string; // 符号权限，如 drwxr-xr-x
  owner: string;
  group: string;
  size: number;
  mod_time: number;
}

/** 构造共享定位 query（pod/container）。 */
function targetQuery(pod: string, container?: string): string {
  const params = new URLSearchParams({ pod });
  if (container) params.set("container", container);
  return params.toString();
}

/** 列出目录真实内容。 */
export async function listFiles(
  sessionId: number,
  pod: string,
  dir: string,
  container?: string,
): Promise<FileEntry[]> {
  const params = new URLSearchParams({ pod, path: dir });
  if (container) params.set("container", container);
  return http.get<unknown, FileEntry[]>(
    `/sandboxes/${sessionId}/files/list?${params.toString()}`,
  );
}

/** 读取文本文件内容。 */
export async function readFile(
  sessionId: number,
  pod: string,
  pathValue: string,
  container?: string,
): Promise<string> {
  const params = new URLSearchParams({ pod, path: pathValue });
  if (container) params.set("container", container);
  const data = await http.get<unknown, { content: string }>(
    `/sandboxes/${sessionId}/files/read?${params.toString()}`,
  );
  return data.content;
}

/** 写入文件内容（编辑器保存）。 */
export async function writeFile(
  sessionId: number,
  pod: string,
  pathValue: string,
  content: string,
  container?: string,
): Promise<void> {
  await http.post(`/sandboxes/${sessionId}/files/write?${targetQuery(pod, container)}`, {
    path: pathValue,
    content,
  });
}

/** 两路径操作（rename/copy/move）。 */
async function twoPath(
  sessionId: number,
  action: "rename" | "copy" | "move",
  pod: string,
  from: string,
  to: string,
  container?: string,
): Promise<void> {
  await http.post(
    `/sandboxes/${sessionId}/files/${action}?${targetQuery(pod, container)}`,
    { from, to },
  );
}

export const renameFile = (
  sessionId: number, pod: string, from: string, to: string, container?: string,
) => twoPath(sessionId, "rename", pod, from, to, container);

export const copyFile = (
  sessionId: number, pod: string, from: string, to: string, container?: string,
) => twoPath(sessionId, "copy", pod, from, to, container);

export const moveFile = (
  sessionId: number, pod: string, from: string, to: string, container?: string,
) => twoPath(sessionId, "move", pod, from, to, container);

/** 修改数字权限（如 0755）。 */
export async function chmodFile(
  sessionId: number,
  pod: string,
  pathValue: string,
  mode: string,
  container?: string,
): Promise<void> {
  await http.post(
    `/sandboxes/${sessionId}/files/chmod?${targetQuery(pod, container)}`,
    { path: pathValue, mode },
  );
}

/** 单路径操作（mkdir/touch/remove）。 */
async function singlePath(
  sessionId: number,
  action: "mkdir" | "touch" | "remove",
  pod: string,
  pathValue: string,
  container?: string,
): Promise<void> {
  await http.post(
    `/sandboxes/${sessionId}/files/${action}?${targetQuery(pod, container)}`,
    { path: pathValue },
  );
}

export const makeDir = (
  sessionId: number, pod: string, pathValue: string, container?: string,
) => singlePath(sessionId, "mkdir", pod, pathValue, container);

export const makeFile = (
  sessionId: number, pod: string, pathValue: string, container?: string,
) => singlePath(sessionId, "touch", pod, pathValue, container);

export const removeFile = (
  sessionId: number, pod: string, pathValue: string, container?: string,
) => singlePath(sessionId, "remove", pod, pathValue, container);

/** 同名处理策略。 */
export type OnExist = "skip" | "replace" | "keep_both";

/**
 * 上传文件/文件夹。
 * items 为 { relativePath, file }：relativePath 保留目录结构（文件夹上传），
 * 后端按相对路径打成 tar 在容器内解包。
 */
export async function uploadFiles(
  sessionId: number,
  pod: string,
  destDir: string,
  items: Array<{ relativePath?: string; file: File }>,
  onExist: OnExist = "replace",
  container?: string,
): Promise<{ written: number; skipped: number }> {
  const form = new FormData();
  items.forEach(({ relativePath, file }) => {
    // 字段名固定 files；用第三参数携带相对路径（含文件夹结构）。
    form.append("files", file, normalizeUploadName(relativePath, file.name));
  });

  const params = new URLSearchParams({ pod, path: destDir, on_exist: onExist });
  if (container) params.set("container", container);

  return http.post(
    `/sandboxes/${sessionId}/files/upload?${params.toString()}`,
    form,
    { headers: { "Content-Type": "multipart/form-data" } },
  );
}

/** 下载文件/目录：带鉴权头取 blob 后触发浏览器保存（目录由后端转 zip）。 */
export async function downloadFile(
  sessionId: number,
  pod: string,
  pathValue: string,
  container?: string,
): Promise<void> {
  const params = new URLSearchParams({ pod, path: pathValue });
  if (container) params.set("container", container);

  const resp = await fetch(
    `/api/v1/sandboxes/${sessionId}/files/download?${params.toString()}`,
    {
      method: "GET",
      headers: authorizationHeaders(),
    },
  );
  if (!resp.ok) {
    throw new Error(`下载失败（${resp.status}）`);
  }

  const filename = filenameFromDisposition(resp.headers.get("Content-Disposition"))
    || fallbackName(pathValue);
  const blob = await resp.blob();
  triggerBlobDownload(blob, filename);
}

/** 构造 Authorization 头（fetch 不经过 axios 拦截器）。 */
function authorizationHeaders(): Record<string, string> {
  const token = tokenStore.get();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/** 规范上传名：优先 webkitRelativePath，去除前导 ./。 */
function normalizeUploadName(relativePath: string | undefined, fallback: string): string {
  const name = (relativePath || fallback).replace(/\\/g, "/").replace(/^\.\//, "");
  return name;
}

/** 从 Content-Disposition 解析 filename*（RFC 5987）或 filename。 */
function filenameFromDisposition(disposition: string | null): string {
  if (!disposition) return "";
  const star = /filename\*=UTF-8''([^;]+)/i.exec(disposition);
  if (star?.[1]) return decodeURIComponent(star[1]);
  const plain = /filename="?([^";]+)"?/i.exec(disposition);
  return plain?.[1] || "";
}

/** 触发浏览器下载。 */
function triggerBlobDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** 兜底文件名。 */
function fallbackName(p: string): string {
  const base = p.split("/").filter(Boolean).pop() || "download";
  return base;
}
