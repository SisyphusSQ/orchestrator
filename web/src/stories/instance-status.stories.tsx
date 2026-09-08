import type { Meta, StoryObj } from "@storybook/react-vite";
import { InstanceTag } from "../components/common";
import { lagging, replica, stopped } from "./fixtures";

const meta = {
  title: "业务组件/实例状态",
  id: "instance-status",
  component: InstanceTag,
  args: { instance: replica },
  parameters: {
    docs: {
      description: {
        component:
          "与拓扑和实例列表共用状态判定。连接异常、采集过期和复制状态优先于停机标记，未知延迟不会显示为正常。",
      },
    },
  },
} satisfies Meta<typeof InstanceTag>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Healthy: Story = { name: "正常" };
export const Lagging: Story = { name: "复制延迟", args: { instance: lagging } };
export const Stopped: Story = { name: "复制停止", args: { instance: stopped } };
export const Unreachable: Story = {
  name: "连接异常",
  args: { instance: { ...replica, IsLastCheckValid: false } },
};
export const Stale: Story = {
  name: "信息过期",
  args: { instance: { ...replica, IsRecentlyChecked: false } },
};
export const UnknownLag: Story = {
  name: "延迟未知",
  args: {
    instance: { ...replica, ReplicationLagSeconds: { Int64: 0, Valid: false } },
  },
};
export const Downtime: Story = {
  name: "停机维护",
  args: { instance: { ...replica, IsDowntimed: true } },
};
