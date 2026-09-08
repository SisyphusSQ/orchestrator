import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { Button } from "antd";
import { http, HttpResponse } from "msw";
import { expect, userEvent, within } from "storybook/test";
import { InstanceDrawer } from "../components/instance-drawer";
import { replica, stopped } from "./fixtures";

function DrawerExample() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button onClick={() => setOpen(true)}>查看实例</Button>
      <InstanceDrawer
        selected={open ? replica.Key : undefined}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
const meta = {
  title: "业务组件/实例详情",
  id: "instance-drawer",
  component: DrawerExample,
  play: async ({ canvasElement }) => {
    await userEvent.click(
      within(canvasElement).getByRole("button", { name: "查看实例" }),
    );
    await expect(
      await within(canvasElement.ownerDocument.body).findByText("实例详情", {
        exact: true,
      }),
    ).toBeVisible();
  },
} satisfies Meta<typeof DrawerExample>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Healthy: Story = { name: "健康副本与 GTID" };
export const ReplicationError: Story = {
  name: "复制错误诊断",
  beforeEach({ msw }) {
    msw.use(http.get("/api/instance/*", () => HttpResponse.json(stopped)));
  },
};
export const ReadOnly: Story = {
  name: "只读详情",
  parameters: { webConfig: { authorizedForAction: false } },
};
