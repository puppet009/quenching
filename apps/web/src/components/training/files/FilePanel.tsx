// FilePanel.tsx 服务器窗口内可收起的文件管理器（文件管理器第二批）。
//
// 工具栏（上传文件/文件夹、新建、刷新）+ 可编辑地址栏（点路径直接改、回车
// 跳转）+ 文件列表。列表数据来自容器真实文件系统；单击文件夹进入，单击
// 文本文件打开在线编辑器。支持拖拽上传。
import { useCallback, useEffect, useRef, useState } from "react";
import {
  listFiles,
  uploadFiles,
  type FileEntry,
  type OnExist,
} from "../../../services/files";
import { FileRow } from "./FileRow";
import { FileEditor } from "./FileEditor";

export interface FilePanelProps {
  sessionId: number;
  pod: string;
  container?: string;
  /** 终端当前目录；变化时更新文件列表，保留已打开的编辑器。 */
  directory: string;
  workingDir?: string;
  deploymentDir?: string;
  configDir?: string;
}

/** 判断文件名是否像可编辑文本（用于单击直接进编辑器）。 */
function isTextLike(name: string): boolean {
  return /\.(txt|yaml|yml|json|conf|cfg|ini|log|sh|md|env|toml|xml|js|ts|py|java|go|sql|service|conf)$|(^|\/)(Dockerfile|Makefile|README|LICENSE|hosts|resolv\.conf)$/i.test(name);
}

export function FilePanel({ sessionId, pod, container, directory, workingDir, deploymentDir, configDir }: FilePanelProps) {
  const [cwd, setCwd] = useState(directory);
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [pathDraft, setPathDraft] = useState("");
  const [uploading, setUploading] = useState(false);
  const [notice, setNotice] = useState("");
  const [editorPath, setEditorPath] = useState<string | null>(null);
  const [onExist, setOnExist] = useState<OnExist>("replace");
  const [uploadMenuOpen, setUploadMenuOpen] = useState(false);
  const directoryRequestRef = useRef(0);
  const mountedRef = useRef(true);
  const cwdRef = useRef(cwd);
  cwdRef.current = cwd;
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; directoryRequestRef.current++; };
  }, []);

  // 监听 FileMenu 的“编辑”请求：路径匹配本面板（同一 Pod）时切到编辑器。
  useEffect(() => {
    const handler = (e: Event) => {
      const pathValue = (e as CustomEvent<string>).detail;
      if (typeof pathValue === "string") setEditorPath(pathValue);
    };
    window.addEventListener("quenching:edit-file", handler);
    return () => window.removeEventListener("quenching:edit-file", handler);
  }, []);

  const fileInputRef = useRef<HTMLInputElement>(null);
  const dirInputRef = useRef<HTMLInputElement>(null);
  const dragDepth = useRef(0);
  const [dragOver, setDragOver] = useState(false);

  const refresh = useCallback(async (targetDir: string) => {
    if (!mountedRef.current) return;
    const request = ++directoryRequestRef.current;
    const current = () => mountedRef.current && request === directoryRequestRef.current;
    setLoading(true);
    setError("");
    cwdRef.current = targetDir;
    setCwd(targetDir);
    setEntries([]);
    try {
      const rows = await listFiles(sessionId, pod, targetDir, container);
      if (current()) setEntries(rows);
    } catch (err) {
      if (current()) setError(err instanceof Error ? err.message : "加载目录失败");
    } finally {
      if (current()) setLoading(false);
    }
  }, [sessionId, pod, container]);

  useEffect(() => {
    void refresh(directory);
    return () => { directoryRequestRef.current++; };
  }, [directory, refresh]);

  const goUp = useCallback(() => {
    if (cwd === "/") return;
    const parts = cwd.split("/").filter(Boolean);
    parts.pop();
    void refresh("/" + parts.join("/"));
  }, [cwd, refresh]);

  const commitPathEdit = useCallback(() => {
    let next = pathDraft.trim();
    setEditingPath(null);
    if (!next) return;
    if (!next.startsWith("/")) next = cwd === "/" ? "/" + next : cwd + "/" + next;
    void refresh(next);
  }, [pathDraft, cwd, refresh]);

  const startUpload = useCallback((list: FileList | null) => {
    if (!list || list.length === 0) return;
    const items = Array.from(list).map((file) => ({
      // 文件夹选择器（webkitdirectory）下 webkitRelativePath 形如
      // “文件夹/子目录/a.txt”，据此保留整个目录结构；普通文件上传仅文件名。
      relativePath: (file as File & { webkitRelativePath?: string }).webkitRelativePath || undefined,
      file,
    }));
    setUploading(true);
    setNotice("")
    uploadFiles(sessionId, pod, cwd, items, onExist, container)
      .then((res) => {
        setNotice(res.skipped > 0 ? `已上传 ${res.written}，跳过 ${res.skipped}` : "上传完成")
        return refresh(cwdRef.current)
      })
      .catch((err) => setError(err instanceof Error ? err.message : "上传失败"))
      .finally(() => setUploading(false));
  }, [sessionId, pod, cwd, onExist, container, refresh]);

  // 拖拽上传（含文件夹，DataTransferItem 的 webkitGetAsEntry 可保留目录结构）。
  const handleDrop = useCallback(async (e: React.DragEvent) => {
    e.preventDefault();
    dragDepth.current = 0;
    setDragOver(false);

    const items = await collectDroppedItems(e.dataTransfer);
    if (items.length === 0) return;

    setUploading(true);
    uploadFiles(sessionId, pod, cwd, items, onExist, container)
      .then(() => refresh(cwdRef.current))
      .catch((err) => setError(err instanceof Error ? err.message : "拖拽上传失败"))
      .finally(() => setUploading(false));
  }, [sessionId, pod, cwd, onExist, container, refresh]);

  // 单击条目：文件夹进入，文本文件进编辑器。
  const handleOpen = useCallback((entry: FileEntry) => {
    if (entry.is_dir) {
      void refresh(entry.path);
      return;
    }
    if (isTextLike(entry.name)) {
      setEditorPath(entry.path);
    } else {
      setNotice("该文件类型不支持在线编辑，可使用下载查看");
    }
  }, [refresh]);

  const changed = useCallback(() => {
    void refresh(cwd);
  }, [refresh, cwd]);
  const shortcuts = [
    { label: deploymentDir && deploymentDir !== workingDir ? "项目目录" : "初始目录", path: workingDir },
    { label: "部署目录", path: deploymentDir },
    { label: "配置目录", path: configDir },
  ].filter((item, index, all) => item.path && all.findIndex((other) => other.path === item.path) === index);

  if (editorPath) {
    return (
      <FileEditor
        sessionId={sessionId}
        pod={pod}
        container={container}
        path={editorPath}
        onClose={() => setEditorPath(null)}
      />
    );
  }

  return (
    <div
      className={`flex flex-col h-full bg-[#f6f7f9] relative ${dragOver ? "ring-2 ring-brand-400 ring-inset" : ""}`}
      onDragEnter={(e) => { e.preventDefault(); dragDepth.current++; setDragOver(true); }}
      onDragOver={(e) => e.preventDefault()}
      onDragLeave={() => { dragDepth.current--; if (dragDepth.current <= 0) setDragOver(false); }}
      onDrop={handleDrop}
    >
      {/* 工具栏 */}
      <div className="flex items-center gap-1.5 px-2 py-1.5 border-b border-ink-200 bg-white">
        <button
          onClick={goUp}
          className="w-7 h-7 rounded-md text-ink-500 hover:bg-ink-100 flex items-center justify-center"
          title="返回上级"
        >
          ←
        </button>
        <div className="relative">
          <button
            onClick={() => setUploadMenuOpen((v) => !v)}
            disabled={uploading}
            className="h-7 px-2.5 rounded-md bg-brand-500 hover:bg-brand-600 text-white text-[11px] font-medium flex items-center gap-1 disabled:opacity-60"
            title="上传文件或整个文件夹（也可直接拖入）"
          >
            ＋ 上传 ▾
          </button>
          {uploadMenuOpen && (
            <>
              <div className="absolute left-0 top-8 w-[140px] bg-white border border-ink-200 rounded-lg shadow-lg py-1 z-30">
                <button
                  onClick={() => { setUploadMenuOpen(false); fileInputRef.current?.click(); }}
                  className="w-full text-left px-3 py-1.5 text-[12px] text-ink-700 hover:bg-brand-50"
                >
                  📄 上传文件
                </button>
                <button
                  onClick={() => { setUploadMenuOpen(false); dirInputRef.current?.click(); }}
                  className="w-full text-left px-3 py-1.5 text-[12px] text-ink-700 hover:bg-brand-50"
                >
                  📂 上传文件夹
                </button>
              </div>
              {/* 点击外部收起 */}
              <div className="fixed inset-0 z-20" onClick={() => setUploadMenuOpen(false)} />
            </>
          )}
        </div>
        <button
          className="h-7 px-2 rounded-md border border-ink-200 text-[11px] text-ink-600 hover:bg-ink-100"
          onClick={() => void refresh(cwd)}
          title="刷新"
        >
          ⟳
        </button>
        <select
          value={onExist}
          onChange={(e) => setOnExist(e.target.value as OnExist)}
          className="ml-auto h-7 text-[10px] border border-ink-200 rounded-md px-1 text-ink-500 bg-white"
          title="同名处理"
        >
          <option value="replace">覆盖</option>
          <option value="skip">跳过</option>
          <option value="keep_both">保留两者</option>
        </select>
      </div>

      {/* 隐藏的文件/文件夹选择器 */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={(e) => { startUpload(e.target.files); e.target.value = ""; }}
      />
      <input
        ref={dirInputRef}
        type="file"
        // @ts-expect-error 非标准但主流浏览器支持文件夹选择
        webkitdirectory=""
        directory=""
        multiple
        className="hidden"
        onChange={(e) => { startUpload(e.target.files); e.target.value = ""; }}
      />

      {/* 可编辑地址栏 */}
      <div className="flex items-center gap-2 px-2.5 py-1.5 border-b border-ink-200 bg-[#fbfbfc]">
        <span className="text-[12px]">📁</span>
        {editingPath === "cwd" ? (
          <input
            autoFocus
            value={pathDraft}
            onChange={(e) => setPathDraft(e.target.value)}
            onBlur={commitPathEdit}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitPathEdit();
              if (e.key === "Escape") setEditingPath(null);
            }}
            className="flex-1 text-[12px] px-2 py-0.5 border border-brand-300 rounded-md outline-none font-mono"
          />
        ) : (
          <button
            onClick={() => { setEditingPath("cwd"); setPathDraft(cwd); }}
            title="点击编辑路径，回车跳转"
            className="flex-1 text-left text-[12px] font-mono text-ink-700 px-1.5 py-0.5 rounded hover:bg-ink-100"
          >
            {cwd}
          </button>
        )}
      </div>

      {shortcuts.length > 0 && (
        <div className="flex flex-wrap gap-1 px-2.5 py-1.5 border-b border-ink-200 bg-[#fbfbfc]">
          {shortcuts.map((shortcut) => (
            <button
              key={shortcut.path}
              type="button"
              title={shortcut.path}
              onClick={() => { if (shortcut.path) void refresh(shortcut.path); }}
              className="rounded border border-ink-200 bg-white px-2 py-1 text-[11px] text-ink-600 hover:bg-brand-50"
            >
              {shortcut.label}
            </button>
          ))}
        </div>
      )}

      {/* 文件列表 */}
      <div className="flex-1 overflow-y-auto py-1">
        {loading ? (
          <div className="h-full flex items-center justify-center text-[12px] text-ink-400">加载中…</div>
        ) : error ? (
          <div className="px-3 py-6 text-center text-[12px] text-danger-600">{error}</div>
        ) : entries.length === 0 ? (
          <div className="px-3 py-10 text-center text-[12px] text-ink-400">空目录，可拖入或点 ＋ 上传</div>
        ) : (
          entries.map((entry) => (
            <FileRow
              key={entry.path}
              entry={entry}
              sessionId={sessionId}
              pod={pod}
              container={container}
              onOpen={handleOpen}
              onChanged={changed}
            />
          ))
        )}
      </div>

      {/* 上传/提示浮层 */}
      {uploading && (
        <div className="absolute inset-0 bg-white/70 flex items-center justify-center text-[12px] text-brand-600 font-medium">
          正在传输…
        </div>
      )}
      {!uploading && !error && notice && (
        <div className="absolute bottom-3 left-1/2 -translate-x-1/2 bg-ink-800 text-white text-[11px] px-3 py-1.5 rounded-lg shadow-lg">
          {notice}
        </div>
      )}
      {dragOver && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <span className="bg-brand-500/90 text-white text-[13px] font-medium px-4 py-2 rounded-xl">松开以上传到 {cwd}</span>
        </div>
      )}
    </div>
  );
}

/**
 * 从拖拽 DataTransfer 收集文件，尽量保留目录结构。
 * 优先用 webkitGetAsEntry 递归展开文件夹；不支持时退化为 files（平铺）。
 */
async function collectDroppedItems(
  dataTransfer: DataTransfer,
): Promise<Array<{ relativePath?: string; file: File }>> {
  const entries = Array.from(dataTransfer.items || [])
    .map((item) => (typeof item.webkitGetAsEntry === "function" ? item.webkitGetAsEntry() : null))
    .filter((e): e is FileSystemEntry => e !== null);

  if (entries.length === 0) {
    return Array.from(dataTransfer.files || []).map((file) => ({ file }));
  }

  const out: Array<{ relativePath?: string; file: File }> = [];
  for (const entry of entries) {
    await walkEntry(entry, entry.name, out);
  }
  return out;
}

/** 递归遍历 FileSystemEntry 收集文件（相对路径保留目录结构）。 */
function walkEntry(
  entry: FileSystemEntry,
  relPath: string,
  out: Array<{ relativePath?: string; file: File }>,
): Promise<void> {
  if (entry.isFile) {
    return new Promise((resolve) => {
      (entry as FileSystemFileEntry).file((file) => {
        out.push({ relativePath: relPath, file });
        resolve();
      }, () => resolve());
    });
  }
  if (entry.isDirectory) {
    const reader = (entry as FileSystemDirectoryEntry).createReader();
    return new Promise((resolve) => {
      const readAll = () => {
        reader.readEntries(async (batch) => {
          if (!batch.length) {
            resolve();
            return;
          }
          for (const child of batch) {
            await walkEntry(child, `${relPath}/${child.name}`, out);
          }
          readAll(); // readEntries 每次可能只返回部分，需读到空
        }, () => resolve());
      };
      readAll();
    });
  }
  return Promise.resolve();
}
