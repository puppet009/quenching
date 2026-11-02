const paths: Record<string, string> = {
    commands: "m5 7 5 5-5 5 M13 17h6", metrics: "M4 18V6 M4 18h16 M8 14l4-5 4 2 4-7",
    health: "M3 12h4l3-7 4 14 3-7h4", logs: "M6 3h9l4 4v14H6z M9 11h7 M9 15h7",
    server: "M4 4h16v7H4z M4 14h16v7H4z M7 7h.01 M7 17h.01 M11 7h6 M11 17h6",
    assistant: "M8 4h8l4 4v10H4V8z M9 11h.01 M15 11h.01 M9 15h6 M12 4V1",
    folder: "M3 6h7l2 3h9v11H3z", clock: "M12 8v5l3 2 M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0",
    alert: "M18 8a6 6 0 0 0-12 0v7l-2 2h16l-2-2z M10 21h4",
};
export function WorkbenchIcon({ name, size = 20 }: {
    name: string;
    size?: number;
}) {
    return <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name] ?? paths.server}/></svg>;
}
