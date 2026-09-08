import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Storybook 不继承应用的 API 代理，模拟请求无法转发到真实后端。
export default defineConfig({
  plugins: [react()],
  build: {
    rolldownOptions: { output: { strictExecutionOrder: true } },
  },
});
