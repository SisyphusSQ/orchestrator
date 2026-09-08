import { expect, test, type Page } from "@playwright/test";

// Fixtures are confined to the test browser. The application has no mock mode.
const cluster = "mysql-primary.test:3306";
const master = {
  Key: { Hostname: "mysql-primary.test", Port: 3306 },
  MasterKey: { Hostname: "", Port: 0 },
  ClusterName: cluster,
  Version: "8.0.46",
  InstanceAlias: "主库",
  ReadOnly: false,
  IsCoMaster: false,
  IsLastCheckValid: true,
  IsRecentlyChecked: true,
  IsUpToDate: true,
  ReplicationDepth: 0,
  ReplicationSQLThreadRuning: false,
  ReplicationIOThreadRuning: false,
  ReplicationLagSeconds: { Int64: 0, Valid: true },
  DataCenter: "北京",
  PhysicalEnvironment: "测试",
  LogBinEnabled: true,
  LogReplicationUpdatesEnabled: true,
  UsingOracleGTID: true,
  Binlog_format: "ROW",
  SelfBinlogCoordinates: { LogFile: "binlog.000001", LogPos: 120 },
};
const replica = {
  ...master,
  Key: { Hostname: "mysql-replica.test", Port: 3306 },
  MasterKey: master.Key,
  InstanceAlias: "副本",
  ReadOnly: true,
  ReplicationDepth: 1,
  ReplicationSQLThreadRuning: true,
  ReplicationIOThreadRuning: true,
};
const info = {
  ClusterName: cluster,
  ClusterAlias: "订单数据库",
  CountInstances: 2,
  HasAutomatedMasterRecovery: true,
  HasAutomatedIntermediateMasterRecovery: false,
};

async function fixture(page: Page, { readonly = false, prefix = "" } = {}) {
  await page.route("**/api/**", async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith(`${prefix}/api/`)) {
      await route.continue();
      return;
    }
    const path = decodeURIComponent(
      new URL(route.request().url()).pathname.replace(`${prefix}/api`, ""),
    );
    if (route.request().method() !== "GET")
      throw new Error(`Unexpected mutation: ${path}`);
    let data: unknown = [];
    if (path === "/web-config")
      data = {
        urlPrefix: prefix,
        userId: "reviewer",
        authorizedForAction: !readonly,
        agentsEnabled: false,
        pseudoGTIDEnabled: true,
        removeTextFromHostnameDisplay: "",
        webMessage: "",
        auditPageSize: 20,
        auditEnabled: true,
      };
    else if (path === "/check-global-recoveries")
      data = { Code: "OK", Details: "enabled" };
    else if (path === "/clusters-info") data = [info];
    else if (path.startsWith("/cluster-info/")) data = info;
    else if (path.startsWith("/cluster/")) data = [master, replica];
    else if (path.startsWith("/instance/"))
      data = path.includes("mysql-replica") ? replica : master;
    await route.fulfill({ json: data });
  });
}

test("overview, topology, drawer and refresh retain navigation state", async ({
  page,
}) => {
  const failures: string[] = [];
  page.on("pageerror", (error) => failures.push(error.message));
  await fixture(page);
  await page.goto("/web/clusters");
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  await page.getByRole("link", { name: "订单数据库", exact: true }).click();
  await expect(page.locator(".database-node")).toHaveCount(2);
  await page
    .getByRole("button", { name: "mysql-replica.test:3306", exact: true })
    .click();
  await expect(page.getByText("实例详情", { exact: true })).toBeVisible();
  await page.getByRole("tab", { name: "复制与 GTID" }).click();
  await page.evaluate(() => window.dispatchEvent(new Event("orch:refresh")));
  await expect(page.getByRole("tab", { name: "复制与 GTID" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(failures).toEqual([]);
});

test("HTTP 200 business failure never displays success or replays action", async ({
  page,
}) => {
  await fixture(page);
  let writes = 0;
  await page.route("**/api/begin-maintenance/**", async (route) => {
    expect(route.request().method()).toBe("POST");
    writes++;
    expect(decodeURIComponent(route.request().url())).toContain("reason / + %");
    await route.fulfill({
      json: {
        Code: "ERROR",
        Message: "instance already in maintenance",
        Details: { partial: true },
      },
    });
  });
  await page.goto(`/web/cluster/${encodeURIComponent(cluster)}`);
  await page
    .getByRole("button", { name: "mysql-replica.test:3306", exact: true })
    .click();
  await page.getByRole("button", { name: "开始维护", exact: true }).click();
  await page.getByLabel("原因", { exact: true }).fill("reason / + %");
  await expect(page.getByLabel("负责人", { exact: true })).toHaveValue(
    "reviewer",
  );
  await page.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(page.getByText("操作执行失败", { exact: true })).toBeVisible();
  await expect(page.getByText("操作执行成功", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "重新读取状态", exact: true }).click();
  expect(writes).toBe(1);
  await page
    .locator(".ant-modal")
    .getByRole("button", { name: /关\s*闭/ })
    .last()
    .click();
  await page.getByRole("button", { name: "开始维护", exact: true }).click();
  await expect(page.getByLabel("原因", { exact: true })).toHaveValue("");
  await expect(page.getByLabel("负责人", { exact: true })).toHaveValue(
    "reviewer",
  );
});

test("lost acknowledgement is indeterminate and has no retry action", async ({
  page,
}) => {
  await fixture(page);
  let writes = 0;
  await page.route("**/api/disable-global-recoveries", async (route) => {
    writes++;
    await route.abort("connectionreset");
  });
  await page.goto("/web/clusters");
  await page.getByRole("switch", { name: "全局自动恢复" }).click();
  await page.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(page.getByText("操作结果未知", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认执行", exact: true }),
  ).toHaveCount(0);
  expect(writes).toBe(1);
});

test("readonly controls remain disabled and unavailable Agents explain capability", async ({
  page,
}) => {
  await fixture(page, { readonly: true });
  await page.goto("/web/clusters");
  await expect(
    page.getByRole("button", { name: "发现实例", exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByRole("switch", { name: "全局自动恢复" }),
  ).toBeDisabled();
  await page.goto("/web/agents");
  await expect(
    page.getByText("Agent 服务未启用", { exact: true }),
  ).toBeVisible();
});

test("URLPrefix deep link loads and refreshes without losing its route", async ({
  page,
}) => {
  await fixture(page, { prefix: "/orchestrator" });
  await page.goto(`/orchestrator/web/cluster/${encodeURIComponent(cluster)}`);
  await expect(page.locator(".database-node")).toHaveCount(2);
  await page.reload();
  await expect(page.locator(".database-node")).toHaveCount(2);
  await expect(
    page.getByRole("heading", { name: "订单数据库", exact: true }),
  ).toBeVisible();
});

test("small screens keep navigation accessible", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await fixture(page);
  await page.goto("/web/clusters");
  await expect(page.getByRole("heading", { name: "集群总览" })).toBeVisible();
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth),
  ).toBeLessThanOrEqual(390);
});

test("numeric target ports validate and submit exactly one confirmed action", async ({
  page,
}) => {
  await fixture(page);
  let writes = 0;
  await page.route("**/api/discover/mysql-new.test/3306", async (route) => {
    expect(route.request().method()).toBe("POST");
    writes++;
    await route.fulfill({
      json: { Code: "OK", Message: "Instance discovered", Details: master },
    });
  });
  await page.goto("/web/discover");
  await page.getByLabel("主机名或 IP", { exact: true }).fill("mysql-new.test");
  await page.getByRole("button", { name: /发现实例/ }).click();
  const dialog = page.locator(".ant-modal");
  await expect(dialog.getByLabel("目标端口", { exact: true })).toHaveValue(
    "3306",
  );
  await dialog.getByRole("button", { name: "确认执行", exact: true }).click();
  await expect(dialog.getByText("操作执行成功", { exact: true })).toBeVisible();
  expect(writes).toBe(1);
});
