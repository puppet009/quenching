// FileDialogs.tsx 文件操作对话框（文件管理器第二批，对标腾讯云）。
//
// 包含：RenameDialog（重命名）、ChmodDialog（权限：数字 + 读写执行勾选）、
// CopyMoveDialog（复制/移动：填目标路径）、RemoveDialog（删除二次确认）。
import { useState } from "react";
import {
  renameFile,
  chmodFile,
  copyFile,
  moveFile,
  removeFile,
  type FileEntry,
} from "../../../services/files";

/** 通用模态外壳。 */
function ModalShell({
  title,
  onClose,
  children,
  width = 380,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  width?: number;
}) {
  return (
    <div className="fixed inset-0 z-[65] flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-white rounded-2xl shadow-xl overflow-hidden" style={{ width }}>
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-ink-100">
          <h3 className="text-[14px] font-bold text-ink-800">{title}</h3>
          <button onClick={onClose} className="w-7 h-7 rounded-lg text-ink-400 hover:bg-ink-100 flex items-center justify-center">✕</button>
        </div>
        <div className="p-5">{children}</div>
      </div>
    </div>
  );
}

/** 底部错误/操作条。 */
function DialogFooter({
  error,
  busy,
  confirmText,
  onConfirm,
  onCancel,
  danger,
}: {
  error: string;
  busy: boolean;
  confirmText: string;
  onConfirm: () => void;
  onCancel: () => void;
  danger?: boolean;
}) {
  return (
    <div className="mt-5">
      {error && <div className="mb-2 text-[11px] text-danger-600">{error}</div>}
      <div className="flex gap-3">
        <button onClick={onCancel} className="flex-1 py-2.5 rounded-xl bg-ink-100 hover:bg-ink-200 text-[13px] font-medium text-ink-600">取消</button>
        <button
          onClick={onConfirm}
          disabled={busy}
          className={`flex-1 py-2.5 rounded-xl text-white text-[13px] font-medium disabled:opacity-60 ${
            danger ? "bg-danger-500 hover:bg-danger-600" : "bg-brand-500 hover:bg-brand-600"
          }`}
        >
          {busy ? "处理中…" : confirmText}
        </button>
      </div>
    </div>
  );
}

// ===== 重命名 =====

export function RenameDialog({
  entry, sessionId, pod, container, onClose, onDone,
}: DialogProps) {
  const [name, setName] = useState(entry.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const confirm = async () => {
    const trimmed = name.trim();
    if (!trimmed || trimmed.includes("/")) {
      setError("名称非法（不可含 /）");
      return;
    }
    if (trimmed === entry.name) {
      onClose();
      return;
    }
    setBusy(true);
    const to = parentDir(entry.path) + "/" + trimmed;
    renameFile(sessionId, pod, entry.path, to, container)
      .then(onDone)
      .catch((err) => { setError(err.message); setBusy(false); });
  };

  return (
    <ModalShell title="重命名" onClose={onClose}>
      <label className="block text-[11px] text-ink-500 mb-1.5">新名称</label>
      <input
        autoFocus
        value={name}
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && confirm()}
        className="w-full text-[13px] px-3 py-2 border border-ink-200 rounded-lg outline-none focus:border-brand-400"
      />
      <DialogFooter error={error} busy={busy} confirmText="确定" onConfirm={confirm} onCancel={onClose} />
    </ModalShell>
  );
}

// ===== 权限 =====

/** 三类主体与三种权限。 */
const OWNER_GROUPS = [
  { key: "u", label: "所有者" },
  { key: "g", label: "用户组" },
  { key: "o", label: "公共" },
] as const;
const PERM_BITS = [
  { bit: 4, label: "读" },
  { bit: 2, label: "写" },
  { bit: 1, label: "执行" },
] as const;

export function ChmodDialog({
  entry, sessionId, pod, container, onClose, onDone,
}: DialogProps) {
  const initial = (entry.mode || "0644").slice(-3);
  const [mode, setMode] = useState(initial.padStart(3, "0"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  /** 某主体当前是否含某权限位。 */
  const hasBit = (ownerIdx: number, bit: number) =>
    (Number(mode[ownerIdx]) & bit) === bit;

  const toggle = (ownerIdx: number, bit: number) => {
    const cur = Number(mode[ownerIdx]);
    const next = hasBit(ownerIdx, bit) ? cur & ~bit : cur | bit;
    setMode(mode.slice(0, ownerIdx) + String(next) + mode.slice(ownerIdx + 1));
  };

  const confirm = async () => {
    setBusy(true);
    chmodFile(sessionId, pod, entry.path, mode, container)
      .then(onDone)
      .catch((err) => { setError(err.message); setBusy(false); });
  };

  return (
    <ModalShell title="设置权限" onClose={onClose} width={420}>
      <div className="space-y-2">
        {OWNER_GROUPS.map((og, ownerIdx) => (
          <div key={og.key} className="flex items-center gap-3">
            <span className="w-14 text-[12px] text-ink-600">{og.label}</span>
            <div className="flex gap-1.5">
              {PERM_BITS.map((pb) => (
                <button
                  key={pb.bit}
                  onClick={() => toggle(ownerIdx, pb.bit)}
                  className={`px-3 py-1 rounded-md text-[11px] border transition-colors ${
                    hasBit(ownerIdx, pb.bit)
                      ? "bg-brand-500 text-white border-brand-500"
                      : "bg-white text-ink-600 border-ink-200 hover:border-brand-300"
                  }`}
                >
                  {pb.label}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>

      <div className="mt-4 flex items-center gap-2">
        <label className="text-[11px] text-ink-500">数字权限</label>
        <input
          value={mode}
          onChange={(e) => {
            const v = e.target.value.replace(/[^0-7]/g, "").slice(-3);
            setMode(v.padStart(3, "0"));
          }}
          className="w-20 text-[13px] font-mono px-3 py-1.5 border border-ink-200 rounded-lg outline-none focus:border-brand-400 text-center"
        />
      </div>

      <DialogFooter error={error} busy={busy} confirmText="确定" onConfirm={confirm} onCancel={onClose} />
    </ModalShell>
  );
}

// ===== 复制 / 移动 =====

export function CopyMoveDialog({
  mode, entry, sessionId, pod, container, onClose, onDone,
}: Omit<DialogProps, "onDone"> & { mode: "copy" | "move"; onDone: () => void }) {
  const [targetDir, setTargetDir] = useState(parentDir(entry.path));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const confirm = async () => {
    const dir = targetDir.trim();
    if (!dir.startsWith("/")) {
      setError("目标目录必须是绝对路径");
      return;
    }
    setBusy(true);
    const to = dir.replace(/\/+$/, "") + "/" + entry.name;
    const action = mode === "copy" ? copyFile : moveFile;
    action(sessionId, pod, entry.path, to, container)
      .then(onDone)
      .catch((err) => { setError(err.message); setBusy(false); });
  };

  return (
    <ModalShell title={mode === "copy" ? "复制到" : "移动到"} onClose={onClose}>
      <label className="block text-[11px] text-ink-500 mb-1.5">目标目录（绝对路径）</label>
      <input
        autoFocus
        value={targetDir}
        onChange={(e) => setTargetDir(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && confirm()}
        placeholder="如 /tmp 或 /app/backup"
        className="w-full text-[13px] font-mono px-3 py-2 border border-ink-200 rounded-lg outline-none focus:border-brand-400"
      />
      <div className="mt-2 text-[11px] text-ink-400 break-all">
        {mode === "copy" ? "副本将生成于：" : "将移动到："}
        {targetDir.replace(/\/+$/, "") || "/"}/{entry.name}
      </div>
      <DialogFooter
        error={error}
        busy={busy}
        confirmText={mode === "copy" ? "复制" : "移动"}
        onConfirm={confirm}
        onCancel={onClose}
      />
    </ModalShell>
  );
}

// ===== 删除 =====

export function RemoveDialog({
  entry, sessionId, pod, container, onClose, onDone,
}: DialogProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [ack, setAck] = useState(false);

  const confirm = async () => {
    setBusy(true);
    removeFile(sessionId, pod, entry.path, container)
      .then(onDone)
      .catch((err) => { setError(err.message); setBusy(false); });
  };

  return (
    <ModalShell title="确认删除" onClose={onClose}>
      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-full bg-danger-50 flex items-center justify-center flex-shrink-0">
          <span className="text-[18px]">⚠️</span>
        </div>
        <div className="min-w-0">
          <div className="text-[13px] text-ink-800 font-medium break-all">{entry.path}</div>
          <div className="text-[11px] text-ink-500 mt-1">
            {entry.is_dir ? "将递归删除该目录及其中全部内容，" : ""}此操作不可恢复。
          </div>
        </div>
      </div>

      <label className="flex items-center gap-2 mt-4 text-[12px] text-ink-600 cursor-pointer">
        <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
        我已知晓删除后无法恢复
      </label>

      <DialogFooter
        error={error}
        busy={busy}
        confirmText="确认删除"
        onConfirm={() => ack && confirm()}
        onCancel={onClose}
        danger
      />
    </ModalShell>
  );
}

// ===== 公共类型与工具 =====

interface DialogProps {
  entry: FileEntry;
  sessionId: number;
  pod: string;
  container?: string;
  onClose: () => void;
  onDone: () => void;
}

/** 取父目录（根目录下返回空串由调用方拼 /）。 */
function parentDir(p: string): string {
  const idx = p.lastIndexOf("/");
  const parent = p.slice(0, idx);
  return parent === "" ? "/" : parent;
}
