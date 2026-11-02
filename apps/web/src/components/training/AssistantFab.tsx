import { useState } from "react";
import { ChatWindow } from "../ChatWindow";
import { WorkbenchIcon } from "./WorkbenchIcon";
export interface AssistantFabProps {
    sessionId?: number;
    focusServer?: string;
    open: boolean;
    onClose: () => void;
    onUserQuestion?: (text: string) => void;
    onApprovalResolved?: (approved: boolean) => void;
}
// Keep the existing conversation mounted when collapsed, including draft and stream.
export function AssistantFab({ sessionId, focusServer, open, onClose, onUserQuestion, onApprovalResolved }: AssistantFabProps) {
    const [mounted, setMounted] = useState(open);
    if (open && !mounted)
        setMounted(true);
    return <section id="workbench-assistant" className="h-full flex flex-col min-h-0" style={{ display: open ? "flex" : "none" }} aria-hidden={!open}>
    <div className="px-5 py-4 border-b border-ink-100 flex items-center gap-3 shrink-0"><span className="text-brand-600"><WorkbenchIcon name="assistant" size={24}/></span><div className="min-w-0 flex-1"><h2 className="text-sm font-semibold text-ink-800">实训助手</h2><p className="text-xs text-ink-500 mt-1 truncate">{focusServer ? `当前服务 · ${focusServer}` : "班次记忆 · 全局视角"}</p></div><button type="button" title="收起助手" aria-label="收起助手" onClick={onClose} className="w-8 h-8 rounded-lg text-xl text-ink-500 hover:bg-ink-100">×</button></div>
    <div className="flex-1 min-h-0">{mounted && <ChatWindow sessionId={sessionId} conversationType="training_assistant" placeholder="描述问题，或询问排查思路…" height="100%" onUserQuestion={onUserQuestion} onApprovalResolved={onApprovalResolved}/>}</div>
  </section>;
}
