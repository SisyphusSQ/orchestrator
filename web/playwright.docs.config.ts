import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./storybook-docs",
  outputDir: "./storybook-test-results/docs",
  timeout: 120000,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: "http://127.0.0.1:6006",
    ...devices["Desktop Chrome"],
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: "light",
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    reducedMotion: "reduce",
    launchOptions: {
      args: ["--disable-gpu", "--font-render-hinting=none"],
    },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "pnpm preview-storybook",
    url: "http://127.0.0.1:6006/index.json",
    reuseExistingServer: !process.env.CI,
  },
});
