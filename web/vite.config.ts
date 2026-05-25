import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Vite 配置保持最小化，便于本地快速启动竞拍演示页。
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173
  }
});
