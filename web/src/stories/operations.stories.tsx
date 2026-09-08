import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "antd";
import { http, HttpResponse, delay } from "msw";
import { expect, userEvent, within, waitFor } from "storybook/test";
import { useOperation } from "../components/operations";
import { replica } from "./fixtures";

function MaintenanceAction() {
  const operate = useOperation();
  return (
    <Button
      onClick={() =>
        operate(
          "begin-maintenance",
          { instance: replica.Key },
          { reason: "副本版本升级" },
        )
      }
    >
      进入维护
    </Button>
  );
}
const openDialog = async (canvasElement: HTMLElement) => {
  await userEvent.click(
    within(canvasElement).getByRole("button", { name: "进入维护" }),
  );
  return within(
    await within(canvasElement.ownerDocument.body).findByRole("dialog"),
  );
};
const meta = {
  title: "操作流程/维护确认",
  id: "operations",
  component: MaintenanceAction,
  parameters: {
    docs: {
      description: {
        component:
          "展示真实确认流程：核对对象与原因 → 提交一次 → 成功、业务失败或结果未知。所有 POST 均由 Storybook 模拟处理。",
      },
    },
  },
} satisfies Meta<typeof MaintenanceAction>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Confirmation: Story = {
  name: "确认对象与原因",
  play: async ({ canvasElement }) => {
    await openDialog(canvasElement);
  },
};
export const Running: Story = {
  name: "执行中",
  beforeEach({ msw }) {
    msw.use(
      http.post("/api/begin-maintenance/*", async () => {
        await delay("infinite");
        return HttpResponse.json({ Code: "OK" });
      }),
    );
  },
  play: async ({ canvasElement }) => {
    const dialog = await openDialog(canvasElement);
    await userEvent.click(dialog.getByRole("button", { name: "确认执行" }));
    await expect(
      dialog.getByRole("button", { name: /取\s*消/ }),
    ).toBeDisabled();
  },
};
export const Success: Story = {
  name: "执行成功",
  beforeEach({ msw }) {
    msw.use(
      http.post("/api/begin-maintenance/*", () =>
        HttpResponse.json({ Code: "OK", Message: "已进入维护", Details: 427 }),
      ),
    );
  },
  play: async ({ canvasElement }) => {
    const dialog = await openDialog(canvasElement);
    await userEvent.click(dialog.getByRole("button", { name: "确认执行" }));
    await waitFor(() => expect(dialog.getByText("操作执行成功")).toBeVisible());
  },
};
export const BusinessFailure: Story = {
  name: "HTTP 200 业务失败",
  beforeEach({ msw }) {
    msw.use(
      http.post("/api/begin-maintenance/*", () =>
        HttpResponse.json({
          Code: "ERROR",
          Message: "实例已经处于维护状态",
          Details: { owner: "数据库值班" },
        }),
      ),
    );
  },
  play: async ({ canvasElement }) => {
    const dialog = await openDialog(canvasElement);
    await userEvent.click(dialog.getByRole("button", { name: "确认执行" }));
    await waitFor(() => expect(dialog.getByText("操作执行失败")).toBeVisible());
  },
};
export const Unknown: Story = {
  name: "操作结果未知",
  beforeEach({ msw }) {
    msw.use(
      http.post("/api/begin-maintenance/*", () =>
        HttpResponse.json({
          Code: "ERROR",
          ErrorClass: "indeterminate",
          Message: "请求已受理，但暂时无法确认执行结果",
        }),
      ),
    );
  },
  play: async ({ canvasElement }) => {
    const dialog = await openDialog(canvasElement);
    await userEvent.click(dialog.getByRole("button", { name: "确认执行" }));
    await waitFor(() => expect(dialog.getByText("操作结果未知")).toBeVisible());
    await expect(
      dialog.queryByRole("button", { name: "确认执行" }),
    ).not.toBeInTheDocument();
  },
};
export const ReadOnly: Story = {
  name: "只读禁止提交",
  parameters: { webConfig: { authorizedForAction: false } },
  play: async ({ canvasElement }) => {
    const dialog = await openDialog(canvasElement);
    await expect(
      dialog.getByRole("button", { name: "确认执行" }),
    ).toBeDisabled();
  },
};
