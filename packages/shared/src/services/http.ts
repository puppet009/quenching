// 统一请求层：注入 token、解包后端统一响应体 {code,msg,data}、集中错误处理。
// 所有页面只调用这里导出的方法，不要在组件里直接 fetch/axios。
import axios, { AxiosError } from "axios";
import { tokenStore } from "../utils/token";
import { reportBrowserError } from "./telemetry";

export const http = axios.create({
  baseURL: "/api/v1",
  timeout: 15000,
});

// 请求注入 JWT
http.interceptors.request.use((cfg) => {
  const token = tokenStore.get();
  if (token) {
    cfg.headers.Authorization = `Bearer ${token}`;
  }
  return cfg;
});

// 后端业务错误。
export class ApiError extends Error {
  code: number;
  constructor(code: number, msg: string) {
    super(msg);
    this.code = code;
  }
}

http.interceptors.response.use(
  (resp) => {
    const body = resp.data;
    // 统一响应体：code=0 成功，返回 data；否则抛业务错误。
    if (body && typeof body === "object" && "code" in body) {
      if (body.code === 0) return body.data;
      if (body.code === 10002) {
        // 未登录：跳登录（MVP 占位）
        localStorage.removeItem("token");
      }
      throw new ApiError(body.code, body.msg || "请求失败");
    }
    return body;
  },
  (err: AxiosError) => {
    if (!axios.isCancel(err) && (!err.response || err.response.status >= 500)) {
      const path = (err.config?.url ?? "").split(/[?#]/, 1)[0];
      reportBrowserError("request", `HTTP ${err.response?.status ?? "network"} ${err.config?.method ?? "GET"} ${path}`);
    }
    if (err.response?.status === 401) {
      localStorage.removeItem("token");
    }
    return Promise.reject(err);
  }
);
