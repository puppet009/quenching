import { Link } from "react-router-dom";
import { Logo, formatRelTimeLong } from "@quenching/shared";
import { useState } from "react";
import { WorkbenchIcon } from "./WorkbenchIcon";
export type TrainingStatus = "preparing" | "running" | "ended" | "completed";
export interface DockItem {
    id: string;
    label: string;
    icon: string;
    badge?: number;
}
export interface TrainingDesktopProps {
    projectName: string;
    difficulty: "easy" | "medium" | "hard";
    status: TrainingStatus;
    elapsedMs: number;
    showExit?: boolean;
    onExit?: () => void;
    onSubmit?: () => void;
    submitting?: boolean;
    headerExtra?: React.ReactNode;
    dockItems: DockItem[];
    activeDock?: string;
    onDockClick?: (id: string) => void;
    windowItems?: {
        id: string;
        label: string;
        active: boolean;
        minimized: boolean;
    }[];
    onWindowClick?: (id: string) => void;
    sidebar?: React.ReactNode;
    assistantOpen?: boolean;
    onAssistantToggle?: () => void;
    children: React.ReactNode;
}
const statusNames: Record<TrainingStatus, string> = { preparing: "准备中", running: "进行中", ended: "班次已结束", completed: "已完成" };
export function TrainingDesktop({ projectName, difficulty, status, elapsedMs, onExit, onSubmit, submitting, headerExtra, dockItems, activeDock, onDockClick, windowItems = [], onWindowClick, sidebar, assistantOpen, onAssistantToggle, children }: TrainingDesktopProps) {
    const [confirm, setConfirm] = useState<"exit" | "finish" | null>(null);
    return <div className="training-workspace">
    <header className="workbench-header">
      <Link to="/" aria-label="淬火首页"><Logo size="sm" showEn={false}/></Link>
      <div className="workbench-project"><span className="font-semibold truncate">{projectName}</span><span className="text-xs text-ink-500">实训工作台 · {{ easy: "简单", medium: "中等", hard: "困难" }[difficulty]}</span></div>
      <div className="workbench-session"><span className={`workbench-status-dot ${status === "running" ? "is-running" : ""}`}/><span>{statusNames[status]}</span><span className="workbench-time"><WorkbenchIcon name="clock" size={17}/>{formatRelTimeLong(elapsedMs)}</span></div>
      <div className="ml-auto flex items-center gap-3 min-w-0">{headerExtra}
        {status === "running" && onSubmit && <button type="button" className="workbench-primary" onClick={() => setConfirm("finish")}>结束实训</button>}
        <button type="button" className="workbench-secondary disabled:opacity-50" disabled={submitting} onClick={() => setConfirm("exit")}>退出</button>
      </div>
    </header>
    <div className="workbench-body">
      <main className="workbench-desktop" aria-label="实训桌面" data-workbench-desktop>{children}</main>
      <aside className={`workbench-sidebar ${assistantOpen ? "is-assistant" : ""}`} aria-label={assistantOpen ? "实训助手面板" : "环境概览"}>{sidebar}</aside>
    </div>
    <footer className="workbench-dock" aria-label="工作台工具">
      <div className="flex items-center gap-1 shrink-0">{dockItems.map(item => <button type="button" key={item.id} className={`workbench-tool ${activeDock === item.id ? "is-active" : ""}`} aria-pressed={activeDock === item.id} onClick={() => onDockClick?.(item.id)}><WorkbenchIcon name={item.id}/><span>{item.label}</span>{!!item.badge && <span>{item.badge}</span>}</button>)}</div>
      <div className="workbench-tasks">{windowItems.map(item => <button type="button" key={item.id} className={`workbench-task ${item.active && !item.minimized ? "is-active" : ""}`} title={`${item.minimized ? "恢复" : "切换到"} ${item.label}`} onClick={() => onWindowClick?.(item.id)}><WorkbenchIcon name="server" size={16}/><span className="truncate">{item.label}</span><span aria-hidden="true" className={item.minimized ? "opacity-30" : ""}>•</span></button>)}</div>
      <button type="button" className={`workbench-assistant-toggle ${assistantOpen ? "is-active" : ""}`} onClick={onAssistantToggle} aria-expanded={assistantOpen} aria-controls="workbench-assistant"><WorkbenchIcon name="assistant"/><span>实训助手</span></button>
    </footer>
    {confirm && <div className="fixed inset-0 z-[100] flex items-center justify-center bg-ink-900/35 p-6" onKeyDown={event => {
                if (event.key === "Escape")
                    setConfirm(null);
                if (event.key === "Tab") {
                    const buttons = event.currentTarget.querySelectorAll<HTMLButtonElement>("button");
                    const first = buttons[0], last = buttons[buttons.length - 1];
                    if (event.shiftKey && document.activeElement === first) {
                        event.preventDefault();
                        last?.focus();
                    }
                    else if (!event.shiftKey && document.activeElement === last) {
                        event.preventDefault();
                        first?.focus();
                    }
                }
            }}>
      <section role="dialog" aria-modal="true" aria-labelledby="workbench-confirm-title" className="w-[420px] rounded-2xl bg-white p-7 shadow-xl">
        <h2 id="workbench-confirm-title" className="text-lg font-semibold text-ink-800">{confirm === "finish" ? "结束本次实训？" : "退出当前班次？"}</h2>
        <p className="mt-3 text-sm leading-7 text-ink-600">{confirm === "finish" ? "系统将结束整个班次，环境回收和复盘报告在后台继续处理。你可以稍后在我的训练查看。" : "退出将结束本次班次，环境回收和复盘在后台继续，操作记录会保留。再次训练需要创建新的班次。"}</p>
        <div className="mt-6 flex justify-end gap-3"><button type="button" autoFocus className="workbench-secondary" onClick={() => setConfirm(null)}>继续实训</button><button type="button" className="workbench-primary" onClick={() => { const action = confirm; setConfirm(null); if (action === "finish")
            onSubmit?.();
        else
            onExit?.(); }}>{confirm === "finish" ? "结束并生成复盘" : "结束并退出"}</button></div>
      </section>
    </div>}
  </div>;
}
