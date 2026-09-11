import type { Meta, StoryObj } from "@storybook/react-vite";
import { http, HttpResponse, delay } from "msw";
import { expect, waitFor, within } from "storybook/test";
import { Application } from "../app/app";
import { cluster, lagging, primary, webConfig } from "./fixtures";
import { recoverySettingsHandlers } from "./recovery-fixtures";

const meta = {
  title: "控制台/集群总览",
  id: "console",
  component: Application,
  parameters: { layout: "fullscreen" },
} satisfies Meta<typeof Application>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Healthy: Story = {
  name: "运行正常",
  play: async ({ canvasElement }) => {
    await expect(
      await within(canvasElement).findByRole("link", {
        name: "订单数据库",
      }),
    ).toBeVisible();
  },
};
export const TopologyDetail: Story = {
  name: "拓扑详情",
  parameters: {
    route: `/cluster/${encodeURIComponent(primary.ClusterName)}`,
  },
  play: async ({ canvasElement }) => {
    await expect(
      await within(canvasElement).findByText("订单数据库"),
    ).toBeVisible();
    await waitFor(() =>
      expect(canvasElement.querySelectorAll(".database-node")).toHaveLength(3),
    );
  },
};
export const RecoveryConfiguration: Story = {
  name: "恢复策略与 Hook",
  parameters: {
    route: "/recovery-settings",
    msw: { handlers: recoverySettingsHandlers },
  },
  play: async ({ canvasElement }) => {
    await expect(
      await within(canvasElement).findByText("恢复策略与 Hook"),
    ).toBeVisible();
    await expect(
      within(canvasElement).getByText("23 项策略 · 9 个 Hook 阶段"),
    ).toBeVisible();
  },
};
export const NeedsAttention: Story = {
  name: "需要关注",
  beforeEach({ msw }) {
    msw.use(http.get("/api/problems", () => HttpResponse.json([lagging])));
  },
};
export const Empty: Story = {
  name: "尚未发现集群",
  beforeEach({ msw }) {
    msw.use(http.get("/api/clusters-info", () => HttpResponse.json([])));
  },
};
export const ReadOnly: Story = {
  name: "只读用户",
  beforeEach({ msw }) {
    msw.use(
      http.get("/api/web-config", () =>
        HttpResponse.json({ ...webConfig, authorizedForAction: false }),
      ),
    );
  },
  play: async ({ canvasElement }) => {
    await expect(
      await within(canvasElement).findByRole("button", { name: "发现实例" }),
    ).toBeDisabled();
  },
};
export const ReadFailure: Story = {
  name: "读取失败",
  beforeEach({ msw }) {
    msw.use(
      http.get("/api/clusters-info", () =>
        HttpResponse.json(
          { Code: "ERROR", Message: "无法读取集群，请检查后端连接。" },
          { status: 503 },
        ),
      ),
    );
  },
};
export const Loading: Story = {
  name: "加载中",
  beforeEach({ msw }) {
    msw.use(
      http.get("/api/clusters-info", async () => {
        await delay("infinite");
        return HttpResponse.json([cluster]);
      }),
    );
  },
};
