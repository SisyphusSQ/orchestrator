import { expect, test } from "@playwright/test";

test("全部场景渲染完成，交互断言通过，API 始终由模拟处理", async ({
  page,
  request,
}) => {
  const response = await request.get("/index.json");
  expect(response.ok()).toBeTruthy();
  const index = (await response.json()) as {
    entries: Record<string, { id: string; type: string }>;
  };
  const stories = Object.values(index.entries).filter(
    (entry) => entry.type === "story",
  );
  expect(stories.length).toBeGreaterThanOrEqual(30);
  const pageErrors: string[] = [];
  const escapedRequests: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  page.on("response", (response) => {
    if (
      new URL(response.url()).pathname.startsWith("/api/") &&
      !response.fromServiceWorker()
    ) {
      escapedRequests.push(response.url());
    }
  });
  // 在 Storybook 创建事件通道时订阅，避免页面加载太快而漏掉完成事件。
  await page.addInitScript(() => {
    type Channel = {
      on: (event: string, listener: (payload: unknown) => void) => void;
    };
    const state = window as unknown as {
      storyResult?: { storyId: string; status: string };
      storyFailure?: unknown;
    };
    let channel: Channel;
    Object.defineProperty(window, "__STORYBOOK_ADDONS_CHANNEL__", {
      configurable: true,
      get: () => channel,
      set: (value: Channel) => {
        channel = value;
        value.on("storyFinished", (payload) => {
          state.storyResult = payload as typeof state.storyResult;
        });
        for (const event of [
          "storyErrored",
          "storyThrewException",
          "playFunctionThrewException",
        ]) {
          value.on(event, (payload) => {
            state.storyFailure = payload;
          });
        }
      },
    });
  });
  for (const story of stories) {
    await test.step(story.id, async () => {
      await page.goto(`/iframe.html?id=${story.id}&viewMode=story`);
      await page.waitForFunction(
        () => {
          const state = window as unknown as {
            storyResult?: unknown;
            storyFailure?: unknown;
          };
          return !!state.storyResult || !!state.storyFailure;
        },
        undefined,
        { timeout: 15000 },
      );
      const result = await page.evaluate(() => {
        const state = window as unknown as {
          storyResult?: unknown;
          storyFailure?: unknown;
        };
        return { result: state.storyResult, failure: state.storyFailure };
      });
      expect(result.failure, story.id).toBeUndefined();
      expect(result.result).toMatchObject({
        storyId: story.id,
        status: "success",
      });
      await expect(page.locator("#storybook-root")).not.toBeEmpty();
      expect(pageErrors, story.id).toEqual([]);
      expect(escapedRequests, story.id).toEqual([]);
    });
  }
});

test("静态站点可打开文档，未声明的写操作被隔离", async ({ page, request }) => {
  const unmocked = await request.post(
    "/api/begin-maintenance/example/3306/storybook/test",
  );
  expect(unmocked.status()).toBe(404);
  await page.goto("/?path=/docs/instance-status--docs");
  await expect(page.locator("#storybook-preview-iframe")).toBeVisible();
  await expect(
    page
      .frameLocator("#storybook-preview-iframe")
      .getByRole("heading", { name: "实例状态", exact: true }),
  ).toBeVisible();
});

test("总览可以进入模拟拓扑和实例详情", async ({ page }) => {
  const failures: string[] = [];
  page.on("response", (response) => {
    if (
      new URL(response.url()).pathname.startsWith("/api/") &&
      response.status() >= 400
    )
      failures.push(response.url());
  });
  await page.goto("/?path=/story/console--healthy");
  const frame = page.frameLocator("#storybook-preview-iframe");
  await frame.getByRole("link", { name: "订单数据库", exact: true }).click();
  await expect(frame.locator(".database-node")).toHaveCount(3);
  await frame
    .getByRole("button", { name: "orders-replica-01:3306", exact: true })
    .click();
  await frame.getByRole("tab", { name: "复制与 GTID" }).click();
  await expect(frame.getByRole("tab", { name: "复制与 GTID" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  await expect(frame.locator(".query-error")).toHaveCount(0);
  expect(failures).toEqual([]);
});
