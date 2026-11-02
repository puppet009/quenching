// FileRow.tsx 文件列表行（文件管理器第二批）。
//
// 图标 + 名称 + 数字权限 + 大小；悬停右侧出现 ⋯，点击弹出 FileMenu。
// 单击名称按 FilePanel 规则打开（进入目录 / 打开编辑器）。
import { useState } from "react";
import type { FileEntry } from "../../../services/files";
import { FileMenu } from "./FileMenu";

export interface FileRowProps {
  entry: FileEntry;
  sessionId: number;
  pod: string;
  container?: string;
  onOpen: (entry: FileEntry) => void;
  onChanged: () => void;
}

/** 格式化字节为可读大小。 */
function formatSize(size: number): string {
  if (size < 1024) return `${size}B`;
  const units = ["KB", "MB", "GB"];
  let value = size / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value >= 10 || i === 0 ? Math.round(value) : value.toFixed(1)}${units[i]}`;
}

export function FileRow({
  entry, sessionId, pod, container, onOpen, onChanged,
}: FileRowProps) {
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <div className="group relative flex items-center gap-2 mx-1.5 px-2 py-1.5 rounded-lg hover:bg-white hover:shadow-sm">
      <button
        onClick={() => onOpen(entry)}
        className="flex items-center gap-2 flex-1 min-w-0 text-left"
        title={entry.path}
      >
        <span className="text-[15px] flex-shrink-0">{entry.is_dir ? "📂" : fileIcon(entry.name)}</span>
        <span className="text-[12px] text-ink-700 truncate">{entry.name}</span>
      </button>

      <span className="text-[10px] font-mono text-ink-400 hidden sm:block w-[44px] text-right">
        {entry.mode}
      </span>
      <span className="text-[10px] text-ink-400 w-[52px] text-right flex-shrink-0">
        {entry.is_dir ? "—" : formatSize(entry.size)}
      </span>

      <button
        onClick={(e) => { e.stopPropagation(); setMenuOpen(!menuOpen); }}
        className="w-6 h-6 rounded-md text-ink-300 hover:text-brand-600 hover:bg-brand-50 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity flex-shrink-0"
        title="更多操作"
      >
        ⋯
      </button>

      {menuOpen && (
        <FileMenu
          entry={entry}
          sessionId={sessionId}
          pod={pod}
          container={container}
          onClose={() => setMenuOpen(false)}
          onChanged={onChanged}
        />
      )}
    </div>
  );
}

/** 按扩展名给图标。 */
function fileIcon(name: string): string {
  if (/\.(yaml|yml)$/i.test(name)) return "📘";
  if (/\.json$/i.test(name)) return "📙";
  if (/\.(sh)$/i.test(name)) return "📜";
  if (/\.(log)$/i.test(name)) return "🗒️";
  if (/\.(conf|cfg|ini|env)$/i.test(name)) return "🔧";
  if (/\.(zip|tar|gz)$/i.test(name)) return "🗜️";
  return "📄";
}
