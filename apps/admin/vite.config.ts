import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// 管理后台独立 SPA。
// 开发期把 /api 与 /ws 统一代理到 api-gateway（:8080）：
//   g7 起 ws-gateway 已并入 api-gateway，HTTP API 与 WebSocket 均由 8080 同进程承载。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5174,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "/ws": {
        target: "http://localhost:8080",
        ws: true,
        changeOrigin: true,
      },
    },
  },
});
