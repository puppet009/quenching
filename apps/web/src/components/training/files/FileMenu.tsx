// FileMenu.tsx 文件行的 ⋯ 菜单（文件管理器第二批，对标腾讯云）。
//
// 操作：编辑/下载/重命名/权限/复制/移动/删除。文件夹隐藏“编辑”，下载时
// 后端自动转 zip。需要二次输入的操作（重命名/权限/复制/移动/删除）弹出
// FileDialogs 中的对应对话框。
import { useState } from "react";
import type { FileEntry } from "../../../services/files";
import { downloadFile } from "../../../services/files";
import {
  RenameDialog,
  ChmodDialog,
  CopyMoveDialog,
  RemoveDialog,
} from "./FileDialogs";

export interface FileMenuProps {
  entry: FileEntry;
  sessionId: number;
  pod: string;
  container?: string;
  onClose: () => void;
  onChanged: () => void;
}

/** 菜单内对话框种类。 */
type DialogKind = "" | "rename" | "chmod" | "copy" | "move" | "remove";

export function FileMenu({
  entry, sessionId, pod, container, onClose, onChanged,
}: FileMenuProps) {
  const [dialog, setDialog] = useState<DialogKind>("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const handleDownload = async () => {
    setBusy(true);
    setError("");
    try {
      await downloadFile(sessionId, pod, entry.path, container);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "下载失败");
      setBusy(false);
    }
  };

  // 文本文件才显示“编辑”（由 FilePanel 打开编辑器，这里通过自定义事件让
  // 父级 FilePanel 切换到编辑器最直接；为保持解耦，菜单项点击后调用
  // onEdit 透传——通过 window 事件）。
  const openEditor = () => {
    window.dispatchEvent(new CustomEvent("quenching:edit-file", { detail: entry.path }));
    onClose();
  };

  const items: Array<{ key: string; icon: string; label: string; onClick: () => void; danger?: boolean; hide?: boolean }> = [
    { key: "edit", icon: "✏️", label: "编辑", onClick: openEditor, hide: entry.is_dir },
    { key: "download", icon: "⬇️", label: "下载", onClick: () => void handleDownload() },
    { key: "rename", icon: "🔤", label: "重命名", onClick: () => setDialog("rename") },
    { key: "chmod", icon: "🔐", label: "权限", onClick: () => setDialog("chmod") },
    { key: "copy", icon: "📋", label: "复制", onClick: () => setDialog("copy") },
    { key: "move", icon: "📦", label: "移动", onClick: () => setDialog("move") },
    { key: "remove", icon: "🗑️", label: "删除", onClick: () => setDialog("remove"), danger: true },
  ];

  return (
    <>
      {/* 菜单浮层：定位在行下方右侧。 */}
      <div
        className="absolute right-1 top-8 w-[148px] bg-white border border-ink-200 rounded-xl shadow-[0_12px_32px_rgba(0,0,0,0.16)] py-1.5 z-30"
        onPointerDown={(e) => e.stopPropagation()}
      >
        {items.filter((i) => !i.hide).map((item) => (
          <button
            key={item.key}
            onClick={item.onClick}
            disabled={busy}
            className={`w-full flex items-center gap-2.5 px-3 py-1.5 text-[12px] text-left hover:bg-brand-50 disabled:opacity-50 ${
              item.danger ? "text-danger-600 hover:bg-danger-50" : "text-ink-700"
            }`}
          >
            <span className="text-[12px]">{item.icon}</span>
            {item.label}
          </button>
        ))}
        {error && <div className="px-3 pt-1 text-[10px] text-danger-600">{error}</div>}
      </div>

      {/* 点击外部关闭。 */}
      <div className="fixed inset-0 z-20" onClick={onClose} onContextMenu={(e) => { e.preventDefault(); onClose(); }} />

      {/* 各操作对话框。 */}
      {dialog === "rename" && (
        <RenameDialog
          entry={entry}
          sessionId={sessionId}
          pod={pod}
          container={container}
          onClose={() => setDialog("")}
          onDone={() => { setDialog(""); onChanged(); }}
        />
      )}
      {dialog === "chmod" && (
        <ChmodDialog
          entry={entry}
          sessionId={sessionId}
          pod={pod}
          container={container}
          onClose={() => setDialog("")}
          onDone={() => { setDialog(""); onChanged(); }}
        />
      )}
      {(dialog === "copy" || dialog === "move") && (
        <CopyMoveDialog
          mode={dialog}
          entry={entry}
          sessionId={sessionId}
          pod={pod}
          container={container}
          onClose={() => setDialog("")}
          onDone={() => { setDialog(""); onChanged(); }}
        />
      )}
      {dialog === "remove" && (
        <RemoveDialog
          entry={entry}
          sessionId={sessionId}
          pod={pod}
          container={container}
          onClose={() => setDialog("")}
          onDone={() => { setDialog(""); onChanged(); }}
        />
      )}
    </>
  );
}
