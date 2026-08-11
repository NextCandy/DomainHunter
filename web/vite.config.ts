import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 构建产物写入 web/dist，由 go:embed 打包进二进制；
// 开发时把 /api 与 /health 代理到本地运行的 Go 服务。
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/health": "http://127.0.0.1:8080",
    },
  },
});
