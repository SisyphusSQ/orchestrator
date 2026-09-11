import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, userEvent, within } from "storybook/test";
import { RecoverySettingsPage } from "../pages/recovery-settings";
import { recoverySettingsHandlers } from "./recovery-fixtures";

const meta = {
  title: "控制台/恢复配置",
  component: RecoverySettingsPage,
  parameters: {
    layout: "fullscreen",
    msw: { handlers: recoverySettingsHandlers },
  },
} satisfies Meta<typeof RecoverySettingsPage>;
export default meta;
type Story = StoryObj<typeof meta>;

export const GlobalPolicy: Story = {
  name: "全局恢复策略",
  play: async ({ canvasElement }) => {
    await expect(
      await within(canvasElement).findByText("自动主库恢复"),
    ).toBeVisible();
    await expect(
      within(canvasElement).getByText("23 项策略 · 9 个 Hook 阶段"),
    ).toBeVisible();
  },
};

export const Hooks: Story = {
  name: "Hook 覆盖",
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(
      await canvas.findByRole("tab", {
        name: "Pre / Post Hook",
      }),
    );
    const profileReferences = await canvas.findAllByText("通知 DBA");
    await expect(profileReferences.length).toBeGreaterThanOrEqual(2);
    await expect(canvas.getByText("优雅切换前")).toBeVisible();

    await userEvent.click(canvas.getByRole("button", { name: "编辑" }));
    const dialog = await within(canvasElement.ownerDocument.body).findByRole(
      "dialog",
      { name: "编辑 Hook 配置" },
    );
    const modal = within(dialog);
    await expect(
      modal.getByRole("spinbutton", { name: "单命令超时" }),
    ).toHaveValue("30");
    await expect(
      modal.getByRole("spinbutton", { name: "输出上限" }),
    ).toHaveValue("65536");
    await expect(modal.getByText("秒")).toHaveTextContent("秒");
    await expect(modal.getByText("字节")).toHaveTextContent("字节");
  },
};
