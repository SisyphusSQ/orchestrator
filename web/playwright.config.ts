import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: process.env.ORCH_WEB_URL || "http://127.0.0.1:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: process.env.ORCH_WEB_URL
    ? undefined
    : {
        command: "pnpm dev",
        url: "http://127.0.0.1:5173/web/clusters",
        reuseExistingServer: !process.env.CI,
      },
});
