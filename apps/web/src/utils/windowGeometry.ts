export interface DesktopSize {
    width: number;
    height: number;
}
export interface WindowRect extends DesktopSize {
    x: number;
    y: number;
}
export type ResizeEdge = "n" | "s" | "e" | "w" | "ne" | "nw" | "se" | "sw";
export type WindowMode = "floating" | "maximized" | "left" | "right";
export function adaptWindowMode(mode: WindowMode, desktopWidth: number): WindowMode {
    return desktopWidth < 880 && (mode === "left" || mode === "right") ? "maximized" : mode;
}
const clamp = (value: number, min: number, max: number) => Math.min(Math.max(value, min), max);
export function constrainWindow(rect: WindowRect, bounds: DesktopSize): WindowRect {
    const width = clamp(rect.width, Math.min(440, bounds.width), bounds.width);
    const height = clamp(rect.height, Math.min(280, bounds.height), bounds.height);
    return { x: clamp(rect.x, 0, bounds.width - width), y: clamp(rect.y, 0, bounds.height - height), width, height };
}
export function fitWindow(rect: WindowRect, bounds: DesktopSize): WindowRect {
    const margin = bounds.width >= 600 ? 20 : 8;
    return constrainWindow({ ...rect, width: Math.min(rect.width, bounds.width - margin * 2), height: Math.min(rect.height, bounds.height - margin * 2) }, bounds);
}
export function resizeWindow(rect: WindowRect, edge: ResizeEdge, dx: number, dy: number, bounds: DesktopSize): WindowRect {
    const minWidth = Math.min(440, bounds.width), minHeight = Math.min(280, bounds.height);
    let { x, y, width, height } = rect;
    if (edge.includes("e"))
        width = clamp(width + dx, minWidth, bounds.width - x);
    if (edge.includes("s"))
        height = clamp(height + dy, minHeight, bounds.height - y);
    if (edge.includes("w")) {
        const right = x + width;
        x = clamp(x + dx, 0, right - minWidth);
        width = right - x;
    }
    if (edge.includes("n")) {
        const bottom = y + height;
        y = clamp(y + dy, 0, bottom - minHeight);
        height = bottom - y;
    }
    return { x, y, width, height };
}
export function raiseWindow(order: string[], id: string): string[] { return [...order.filter(current => current !== id), id]; }
