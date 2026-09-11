import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";

const screenshotDirectory = fileURLToPath(
  new URL("../../docs/assets/screenshots/", import.meta.url),
);
const stableRenderingCSS = `
  *, *::before, *::after {
    animation: none !important;
    caret-color: transparent !important;
    scroll-behavior: auto !important;
    text-rendering: geometricPrecision !important;
    transition: none !important;
  }
  .ant-select-selector {
    border-color: #d9d9d9 !important;
    border-left-width: 2px !important;
    box-shadow: none !important;
  }
  .topology-options .ant-checkbox-wrapper {
    transform: translateY(0.5px) !important;
  }
`;

async function capture(
  page: Page,
  storyID: string,
  filename: string,
  ready: (page: Page) => Promise<void>,
) {
  await page.clock.setFixedTime(new Date("2026-09-11T09:30:00+08:00"));
  await page.addInitScript((css) => {
    const style = document.createElement("style");
    style.textContent = css;
    document.documentElement.appendChild(style);
  }, stableRenderingCSS);
  await page.goto(`/iframe.html?id=${storyID}&viewMode=story`, {
    waitUntil: "networkidle",
  });
  await page.addStyleTag({ content: stableRenderingCSS });
  await ready(page);
  await page.evaluate(async () => {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
    await document.fonts.ready;
    await new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    );
  });
  await page.waitForTimeout(250);
  await page.screenshot({
    path: `${screenshotDirectory}/${filename}`,
    animations: "disabled",
  });
}

test.beforeAll(async () => {
  await mkdir(screenshotDirectory, { recursive: true });
});

test("生成集群总览文档截图", async ({ page }) => {
  await capture(page, "console--healthy", "cluster-overview.png", async (p) => {
    await expect(
      p.getByRole("heading", { name: "集群总览", exact: true }),
    ).toBeVisible();
    await expect(
      p.getByRole("link", { name: "订单数据库", exact: true }),
    ).toBeVisible();
  });
});

test("生成拓扑详情文档截图", async ({ page }) => {
  await capture(
    page,
    "console--topology-detail",
    "topology-detail.png",
    async (p) => {
      await expect(p.locator(".database-node")).toHaveCount(3);
      await expect(p.getByText("订单数据库", { exact: true })).toBeVisible();
    },
  );
});

test("生成恢复配置文档截图", async ({ page }) => {
  await capture(
    page,
    "console--recovery-configuration",
    "recovery-configuration.png",
    async (p) => {
      await expect(
        p.getByText("恢复策略与 Hook", { exact: true }),
      ).toBeVisible();
      await expect(p.getByText("23 项策略 · 9 个 Hook 阶段")).toBeVisible();
    },
  );
});
