import type { Meta, StoryObj } from "@storybook/react-vite";
import { fn } from "storybook/test";
import { Topology } from "../components/topology";
import {
  healthy,
  maintenance,
  primary,
  problems,
  replica,
  replica2,
} from "./fixtures";

const meta = {
  title: "业务组件/复制拓扑",
  id: "topology",
  component: Topology,
  args: {
    instances: healthy,
    onSelect: fn(),
    compact: false,
    anonymize: false,
    aliases: false,
    colorize: true,
    mode: "smart",
    pools: { "orders-replica-02.example.test:3306": ["只读流量"] },
    maintenance: [],
  },
  argTypes: {
    mode: {
      control: "select",
      options: ["smart", "classic", "gtid", "pseudo"],
    },
  },
  parameters: {
    docs: {
      description: {
        component:
          "可缩放、折叠和拖放的真实拓扑。通过 Controls 切换匿名、别名、紧凑模式和机房颜色；拖放只提出操作，必须在对话框确认。",
      },
    },
  },
} satisfies Meta<typeof Topology>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Healthy: Story = { name: "一主两副本" };
export const NeedsAttention: Story = {
  name: "复制异常",
  args: { instances: problems },
};
export const Maintenance: Story = {
  name: "维护与资源池",
  args: { maintenance: [maintenance] },
};
export const ReadOnly: Story = {
  name: "只读权限",
  parameters: { webConfig: { authorizedForAction: false } },
};
export const CoMaster: Story = {
  name: "双主与下游",
  args: {
    instances: [
      { ...primary, IsCoMaster: true, MasterKey: replica.Key },
      { ...replica, IsCoMaster: true, ReadOnly: false },
      { ...replica2, MasterKey: replica.Key },
    ],
  },
};
