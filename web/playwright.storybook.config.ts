import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./storybook-tests",
  outputDir: "./storybook-test-results",
  timeout: 120000,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:6006",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "pnpm preview-storybook",
    url: "http://127.0.0.1:6006/index.json",
    reuseExistingServer: !process.env.CI,
  },
});
