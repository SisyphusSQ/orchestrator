import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const prefix = (process.env.ORCH_URL_PREFIX || "").replace(/\/$/, "");

export default defineConfig({
  plugins: [react()],
  base: "./",
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      [`${prefix}/api`]: {
        target: process.env.ORCH_API_TARGET || "http://127.0.0.1:3000",
      },
      [`${prefix}/web/access-token`]: {
        target: process.env.ORCH_API_TARGET || "http://127.0.0.1:3000",
      },
    },
  },
  build: {
    rolldownOptions: {
      output: {
        // React and UI dependencies include order-sensitive CommonJS modules.
        strictExecutionOrder: true,
        codeSplitting: {
          includeDependenciesRecursively: false,
          groups: [
            {
              name: "react",
              test: /node_modules\/(react|react-dom|scheduler)\//,
              priority: 30,
            },
            {
              name: "topology",
              test: /node_modules\/(@xyflow|@dagrejs)\//,
              priority: 20,
            },
            {
              name: "vendor",
              test: /node_modules/,
              minSize: 50000,
              maxSize: 600000,
              priority: 10,
            },
          ],
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
  },
});
