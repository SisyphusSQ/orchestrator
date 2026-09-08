import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { InstanceTable } from "../components/instance-table";
import { healthy, problems } from "./fixtures";

const meta = {
  title: "业务组件/实例列表",
  id: "instance-table",
  component: InstanceTable,
  args: { instances: healthy, onSelect: fn() },
} satisfies Meta<typeof InstanceTable>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Healthy: Story = {
  name: "正常与实例选择",
  play: async ({ canvasElement, args }) => {
    await userEvent.click(
      within(canvasElement).getByRole("button", {
        name: "orders-primary.example.test:3306",
      }),
    );
    await expect(args.onSelect).toHaveBeenCalledWith(healthy[0]);
  },
};
export const NeedsAttention: Story = {
  name: "异常副本",
  args: { instances: problems },
};
export const Empty: Story = { name: "空列表", args: { instances: [] } };
export const Loading: Story = {
  name: "加载中",
  args: { instances: [], loading: true },
};
