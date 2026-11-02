import { useLayoutEffect, useRef, useState, type PointerEvent } from "react";
import { adaptWindowMode, constrainWindow, fitWindow, resizeWindow, type DesktopSize, type ResizeEdge, type WindowMode, type WindowRect } from "../../utils/windowGeometry";
export interface WindowProps {
    id: string;
    title: string;
    icon?: string;
    active?: boolean;
    zIndex?: number;
    initialX: number;
    initialY: number;
    width: number;
    height: number;
    minimized?: boolean;
    onFocus?: () => void;
    onClose?: () => void;
    onMinimize?: () => void;
    children: React.ReactNode;
}
export function Window({ id, title, active = false, zIndex = 10, initialX, initialY, width, height, minimized = false, onFocus, onClose, onMinimize, children }: WindowProps) {
    const node = useRef<HTMLDivElement>(null);
    const [rect, setRect] = useState<WindowRect>({ x: initialX, y: initialY, width, height });
    const [bounds, setBounds] = useState<DesktopSize>({ width, height });
    const [mode, setMode] = useState<WindowMode>("floating");
    const gesture = useRef<{
        pointerId: number;
        x: number;
        y: number;
        rect: WindowRect;
        edge?: ResizeEdge;
    } | null>(null);
    // Only the first desktop measurement places a window; focus never resets it.
    useLayoutEffect(() => {
        const parent = node.current?.parentElement;
        if (!parent)
            return;
        let first = true;
        const measure = () => {
            const size = { width: parent.clientWidth, height: parent.clientHeight };
            if (!size.width || !size.height)
                return;
            setBounds(size);
            setRect(current => first ? fitWindow(current, size) : constrainWindow(current, size));
            first = false;
            setMode(current => adaptWindowMode(current, size.width));
        };
        measure();
        const observer = new ResizeObserver(measure);
        observer.observe(parent);
        return () => observer.disconnect();
    }, []);
    const geometry = mode === "maximized" ? { x: 0, y: 0, ...bounds } : mode === "left" || mode === "right"
        ? { x: mode === "left" ? 0 : Math.floor(bounds.width / 2), y: 0, width: mode === "left" ? Math.floor(bounds.width / 2) : Math.ceil(bounds.width / 2), height: bounds.height } : rect;
    const maximize = () => { gesture.current = null; setMode(current => current === "maximized" ? "floating" : "maximized"); };
    const begin = (event: PointerEvent<HTMLElement>, edge?: ResizeEdge) => {
        if (!edge && (event.target as HTMLElement).closest("button"))
            return;
        if (event.button !== 0 || mode !== "floating")
            return;
        event.preventDefault();
        onFocus?.();
        gesture.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, rect, edge };
        event.currentTarget.setPointerCapture(event.pointerId);
    };
    const move = (event: PointerEvent<HTMLElement>) => {
        const start = gesture.current;
        if (!start || start.pointerId !== event.pointerId)
            return;
        const dx = event.clientX - start.x, dy = event.clientY - start.y;
        setRect(start.edge ? resizeWindow(start.rect, start.edge, dx, dy, bounds) : constrainWindow({ ...start.rect, x: start.rect.x + dx, y: start.rect.y + dy }, bounds));
    };
    const end = () => { gesture.current = null; };
    return <section ref={node} role="region" aria-label={title} data-window-id={id} data-window-mode={mode} hidden={minimized} className={`workbench-window ${active ? "is-active" : ""}`} style={{ left: geometry.x, top: geometry.y, width: geometry.width, height: geometry.height, zIndex, display: minimized ? "none" : "flex" }} onPointerDownCapture={onFocus}>
    <div className="workbench-window-title" onPointerDown={event => begin(event)} onPointerMove={move} onPointerUp={end} onPointerCancel={end} onDoubleClick={event => { if (!(event.target as HTMLElement).closest("button"))
        maximize(); }}>
      <span className="workbench-window-marker" aria-hidden="true"/>
      <span className="flex-1 truncate font-semibold">{title}</span>
      <div className="flex items-center gap-1">
        <button type="button" disabled={bounds.width < 880} title="左侧贴靠" aria-label={`左侧贴靠 ${title}`} onClick={() => setMode("left")}>◧</button>
        <button type="button" disabled={bounds.width < 880} title="右侧贴靠" aria-label={`右侧贴靠 ${title}`} onClick={() => setMode("right")}>◨</button>
        {(mode === "left" || mode === "right") && <button type="button" title="还原" aria-label={`还原 ${title}`} onClick={() => setMode("floating")}>↙</button>}
        {onMinimize && <button type="button" title="最小化" aria-label={`最小化 ${title}`} onClick={onMinimize}>−</button>}
        <button type="button" title={mode === "maximized" ? "还原" : "最大化"} aria-label={`${mode === "maximized" ? "还原" : "最大化"} ${title}`} onClick={maximize}>{mode === "maximized" ? "❐" : "□"}</button>
        {onClose && <button type="button" title="关闭" aria-label={`关闭 ${title}`} className="workbench-window-close" onClick={onClose}>×</button>}
      </div>
    </div>
    <div className="flex-1 min-h-0 min-w-0 overflow-hidden">{children}</div>
    {mode === "floating" && (["n", "s", "e", "w", "ne", "nw", "se", "sw"] as const).map(edge => <div key={edge} className={`workbench-resize workbench-resize-${edge}`} data-resize-edge={edge} role={edge === "se" ? "button" : undefined} tabIndex={edge === "se" ? 0 : undefined} aria-label={edge === "se" ? `调整大小 ${title}` : undefined} onKeyDown={event => { const step = event.shiftKey ? 40 : 10; const keys: Record<string, [
            number,
            number
        ]> = { ArrowRight: [step, 0], ArrowLeft: [-step, 0], ArrowDown: [0, step], ArrowUp: [0, -step] }; const delta = keys[event.key]; if (delta) {
            event.preventDefault();
            setRect(current => resizeWindow(current, "se", ...delta, bounds));
        } }} onPointerDown={event => begin(event, edge)} onPointerMove={move} onPointerUp={end} onPointerCancel={end}/>)}
  </section>;
}
