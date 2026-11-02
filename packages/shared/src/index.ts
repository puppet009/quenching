/**
 * @quenching/shared 统一出口
 * web（用户端）与 admin（管理端）共用的 HTTP 基础设施、领域服务、工具与通用组件。
 * 包以 TS 源码形式发布，由各应用的 Vite 直接打包、tsc 直接类型检查，无需预构建。
 */

// HTTP 基础设施（axios 实例 + token 拦截 + 统一解包 + ApiError）
export { http, ApiError } from "./services/http";

// 领域服务
export * from "./services/auth";
export * from "./services/project";
export * from "./services/templates";
export * from "./services/domains";
export * from "./services/defects";
export * from "./services/pitfall";
export * from "./services/feedback";
export * from "./services/metrics";

// 工具
export { tokenStore } from "./utils/token";
export * from "./utils/time";
export { startBrowserRuntime, reportBrowserError } from "./services/telemetry";
export { useServerClock } from "./hooks/useServerClock";

// 通用组件
export { Logo } from "./components/Logo";
export { PasswordInput } from "./components/PasswordInput";
export { DataTable, type DataTableColumn } from "./components/DataTable";
export { Pagination } from "./components/Pagination";
export { RealtimeChart } from "./components/RealtimeChart";
