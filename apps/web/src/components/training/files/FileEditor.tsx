// FileEditor.tsx 在线文本编辑器（文件管理器第二批，对标腾讯云）。
//
// 打开真实文件 → textarea 编辑 → 手动保存或 3 秒自动保存；带未保存状态指示。
// 保存写回真实文件（writeFile）。为避免大文件卡顿，按纯文本区域渲染。
import { useCallback, useEffect, useRef, useState } from "react";
import { readFile, writeFile } from "../../../services/files";

export interface FileEditorProps {
  sessionId: number;
  pod: string;
  container?: string;
  path: string;
  onClose: () => void;
}

/** 自动保存延迟（毫秒）。 */
const AUTOSAVE_DELAY = 3000;

export function FileEditor({
  sessionId, pod, container, path, onClose,
}: FileEditorProps) {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [savedAt, setSavedAt] = useState<number | null>(null);
  const saveTimer = useRef<number | null>(null);

  // 打开真实文件。
  useEffect(() => {
    let cancelled = false;
    readFile(sessionId, pod, path, container)
      .then((text) => {
        if (cancelled) return;
        setContent(text);
      })
      .catch((err) => setError(err instanceof Error ? err.message : "读取失败"))
      .finally(() => !cancelled && setLoading(false));
    return () => { cancelled = true; };
  }, [sessionId, pod, path, container]);

  // 保存。
  const save = useCallback(async (text: string) => {
    setSaving(true);
    setError("");
    try {
      await writeFile(sessionId, pod, path, text, container);
      setDirty(false);
      setSavedAt(Date.now());
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }, [sessionId, pod, path, container]);

  // 修改后重置 3s 自动保存计时。
  const handleChange = (next: string) => {
    setContent(next);
    setDirty(true);
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      void save(next);
    }, AUTOSAVE_DELAY);
  };

  // 卸载：清理定时器（不强制同步保存，自动保存在停顿后已触发）。
  useEffect(() => () => {
    if (saveTimer.current) window.clearTimeout(saveTimer.current);
  }, []);

  const baseName = path.split("/").pop() || path;

  return (
    <div className="flex flex-col h-full bg-white">
      {/* 编辑器标题栏 */}
      <div className="flex items-center gap-2 px-3 h-10 border-b border-ink-200 bg-[#fbfbfc] flex-shrink-0">
        <button
          onClick={onClose}
          className="w-7 h-7 rounded-md text-ink-500 hover:bg-ink-100 flex items-center justify-center"
          title="返回文件列表"
        >
          ←
        </button>
        <span className="text-[13px] font-medium text-ink-800 truncate">{baseName}</span>
        <span className="text-[10px] text-ink-400 font-mono truncate hidden md:block">{path}</span>
        <span className="ml-auto flex items-center gap-2">
          {dirty ? (
            <span className="text-[11px] text-warn-600">● 未保存</span>
          ) : saving ? (
            <span className="text-[11px] text-ink-400">保存中…</span>
          ) : savedAt ? (
            <span className="text-[11px] text-ok-600">✓ 已保存</span>
          ) : null}
          <button
            onClick={() => void save(content)}
            disabled={saving || !dirty}
            className="px-3 py-1.5 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-[11px] font-medium disabled:opacity-50"
          >
            保存
          </button>
        </span>
      </div>

      {/* 编辑区 */}
      {loading ? (
        <div className="flex-1 flex items-center justify-center text-[12px] text-ink-400">正在打开文件…</div>
      ) : (
        <textarea
          value={content}
          onChange={(e) => handleChange(e.target.value)}
          spellCheck={false}
          className="flex-1 w-full resize-none outline-none p-4 font-mono text-[12.5px] leading-relaxed text-ink-800 bg-white"
          style={{ tabSize: 2 }}
        />
      )}

      {error && <div className="px-4 py-2 text-[11px] text-danger-600 border-t border-ink-100">{error}</div>}
    </div>
  );
}
