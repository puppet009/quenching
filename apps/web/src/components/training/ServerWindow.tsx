// ServerWindow.tsx 单个服务器窗口（桌面化第一批 + 文件管理器第二批）。
//
// 一台服务器(Pod)一个窗口：主体为真实终端，顶部可在容器间切换；点标题栏
// 文件夹图标，从右侧滑出可收起的文件管理器（真实文件系统 + 上传下载），
// 与终端同屏。
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { XtermTerminal } from "../XtermTerminal";
import { buildExecWSURL, type ServerTarget } from "../../services/sandbox";
import { tokenStore } from "@quenching/shared";
import { FilePanel } from "./files/FilePanel";
import {WorkbenchIcon} from "./WorkbenchIcon";

export interface ServerWindowProps {
  sessionId: number;
  target: ServerTarget;
  /** 终端命令提交回调（操作时间线留痕用）。 */
  onCommand?: (pod: string, line: string) => void;
}

export function ServerWindow({ sessionId, target, onCommand }: ServerWindowProps) {
  const firstContainer = target.containers[0]?.name ?? "";
  const [container, setContainer] = useState(firstContainer);
  const [showFiles, setShowFiles] = useState(false);
  const [fontSize, setFontSize] = useState(16);
  const [terminalDirectories, setTerminalDirectories] = useState<Record<string, string>>({});
  const terminalKey = `${sessionId}:${target.name}:${container}`;
  const activeTerminalRef = useRef(terminalKey);
  activeTerminalRef.current = terminalKey;
  const selectedContainer = target.containers.find((item) => item.name === container);
  const directory = terminalDirectories[terminalKey] || selectedContainer?.working_dir || "/";
  const onCwdChange = useCallback((cwd: string) => {
    if (activeTerminalRef.current !== terminalKey) return;
    setTerminalDirectories((previous) => previous[terminalKey] === cwd ? previous : { ...previous, [terminalKey]: cwd });
  }, [terminalKey]);
  const onStatusChange = useCallback((status: string) => {
    if (status !== "connecting" || activeTerminalRef.current !== terminalKey) return;
    // A reconnect starts a new shell in its configured workspace.
    setTerminalDirectories((previous) => {
      if (!(terminalKey in previous)) return previous;
      const next = { ...previous };
      delete next[terminalKey];
      return next;
    });
  }, [terminalKey]);
  /** 编辑事件触发时展开文件面板（FileMenu 通过 window 事件请求编辑）。 */
  useEffect(() => {
    const onEditRequest = () => setShowFiles(true);
    window.addEventListener("quenching:edit-file", onEditRequest);
    return () => window.removeEventListener("quenching:edit-file", onEditRequest);
  }, []);

  const token = tokenStore.get() ?? undefined;
  // 以容器为 key：切换容器时整体重建终端与 WS 连接。
  const wsUrl = useMemo(
    () => buildExecWSURL(sessionId, target.name, container || undefined),
    [sessionId, target.name, container],
  );

  return (
    <div className="flex flex-col h-full bg-[#1e1e1e] overflow-hidden">
        {/* 容器切换 + 文件开关条。 */}
        <div className="flex items-center gap-2 px-3 h-11 bg-[#2a2d31] border-b border-black/40 flex-shrink-0">
          {target.containers.length > 1 && (
            <>
              <span className="text-xs text-ink-300">容器</span>
              <select
                aria-label="终端容器"
                value={container}
                onChange={(e) => {
                  const nextContainer = e.target.value;
                  const nextKey = `${sessionId}:${target.name}:${nextContainer}`;
                  // Switching back opens a new shell, rather than resuming the old cwd.
                  setTerminalDirectories((previous) => {
                    if (!(nextKey in previous)) return previous;
                    const next = { ...previous };
                    delete next[nextKey];
                    return next;
                  });
                  setContainer(nextContainer);
                }}
                className="max-w-[140px] min-w-0 text-xs bg-[#21262d] text-ink-200 border border-ink-600 rounded px-2 py-1 outline-none focus:border-brand-500 cursor-pointer"
              >
                {target.containers.map((c) => (
                  <option key={c.name} value={c.name}>
                    {c.name}
                  </option>
                ))}
              </select>
            </>
          )}

          <span aria-label="终端工作目录" title={directory} className="min-w-0 truncate font-mono text-xs text-ink-300">{directory}</span>
          <div className="ml-auto flex items-center gap-2 text-xs text-ink-300"><button type="button" aria-label="缩小终端字体" disabled={fontSize<=12} onClick={()=>setFontSize(size=>size-1)} className="px-2 py-1 hover:bg-white/10 rounded disabled:opacity-30">A−</button><span className="tabular-nums">{fontSize}px</span><button type="button" aria-label="放大终端字体" disabled={fontSize>=22} onClick={()=>setFontSize(size=>size+1)} className="px-2 py-1 hover:bg-white/10 rounded disabled:opacity-30">A+</button></div>
          <button
            onClick={() => setShowFiles(!showFiles)}
            className={`flex items-center gap-2 h-8 px-3 rounded-md text-xs transition-colors ${
              showFiles
                ? "bg-brand-500 text-white"
                : "text-ink-300 hover:bg-[#3a3d42] hover:text-white"
            }`}
            title={showFiles ? "收起文件管理器" : "打开文件管理器"}
          >
            <WorkbenchIcon name="folder" size={17}/>
            <span className="hidden sm:inline">文件</span>
          </button>
        </div>

      <div className="flex flex-1 min-h-0 min-w-0 overflow-hidden">
      <div className="flex-1 min-w-0 flex flex-col">
        <p className="text-xs px-3 py-2 text-amber-200 bg-[#2a2d31]">交互模式：逐条命令和退出码可能无法追踪。需要完整记录时，请使用“命令执行”。</p>
        {/* 真实终端铺满剩余高度。 */}
        <div className="flex-1 min-h-0">
          <XtermTerminal
            key={terminalKey}
            wsUrl={wsUrl}
            token={token}
            target={target.name}
            theme="dark"
            fontSize={fontSize}
            height="100%"
            autoConnect
            onCwdChange={onCwdChange}
            onStatusChange={onStatusChange}
            onCommand={(line) => onCommand?.(target.name, line)}
            className="h-full border-0 rounded-none"
          />
        </div>
      </div>

      {/* 右：可收起文件管理器（固定宽度，覆盖式滑出）。 */}
      {showFiles && (
        <div
          className="flex-shrink-0 border-l border-black/50 bg-white"
          style={{ width: "min(320px, 42%)", height: "100%" }}
        >
          <FilePanel
            key={terminalKey}
            sessionId={sessionId}
            pod={target.name}
            container={container || undefined}
            directory={directory}
            workingDir={selectedContainer?.working_dir}
            deploymentDir={selectedContainer?.deployment_dir}
            configDir={selectedContainer?.config_dir}
          />
        </div>
      )}
      </div>
    </div>
  );
}
