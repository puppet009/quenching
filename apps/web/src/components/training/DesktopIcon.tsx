import { WorkbenchIcon } from "./WorkbenchIcon";
export interface DesktopIconProps {
    name: string;
    phase: string;
    ready: boolean;
    active?: boolean;
    onOpen: () => void;
}
export function DesktopIcon({ name, phase, ready, active = false, onOpen }: DesktopIconProps) {
    return <button type="button" onClick={event => { if (event.detail === 0)
        onOpen(); }} onDoubleClick={onOpen} onKeyDown={event => { if (event.key === "Enter") {
        event.preventDefault();
        onOpen();
    } }} title={`双击连接 ${name}`} className={`workbench-server ${active ? "is-active" : ""}`}>
    <span className="workbench-server-symbol"><WorkbenchIcon name="server" size={28}/><span className={`workbench-status-dot ${ready ? "is-running" : phase === "Failed" ? "is-failed" : ""}`}/></span>
    <span className="font-medium text-sm break-all leading-5">{name}</span>
    <span className="text-xs text-ink-500">{ready ? "可连接" : phase === "Failed" ? "异常" : "准备中"}</span>
  </button>;
}
