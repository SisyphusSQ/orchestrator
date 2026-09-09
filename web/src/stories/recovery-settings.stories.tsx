import type { Meta, StoryObj } from "@storybook/react-vite";
import { http, HttpResponse } from "msw";
import { expect, userEvent, within } from "storybook/test";
import { RecoverySettingsPage } from "../pages/recovery-settings";
import type { RecoveryPolicy } from "../api/types";

const policy: RecoveryPolicy = {
  autoMasterRecovery: false,
  autoIntermediateMasterRecovery: false,
  recoveryIgnoreHostnameFilters: [],
  promotionIgnoreHostnameFilters: ["backup-.*"],
  problemIgnoreHostnameFilters: [],
  failureDetectionPeriodBlockMinutes: 60,
  recoveryPeriodBlockSeconds: 3600,
  reasonableReplicationLagSeconds: 10,
  reasonableMaintenanceReplicationLagSeconds: 20,
  verifyReplicationFilters: false,
  failMasterPromotionOnLagMinutes: 0,
  sqlThreadPromotionPolicy: "allow",
  recoverNonWriteableMaster: false,
  coMasterRecoveryMustPromoteOtherCoMaster: true,
  detachLostReplicasAfterMasterFailover: true,
  applyMySQLPromotionAfterMasterFailover: true,
  preventCrossDataCenterMasterFailover: false,
  preventCrossRegionMasterFailover: false,
  masterFailoverDetachReplicaMasterHost: false,
  postponeReplicaRecoveryOnLagMinutes: 0,
  enforceExactSemiSyncReplicas: false,
  recoverLockedSemiSyncMaster: false,
  reasonableLockedSemiSyncMasterSeconds: 0,
};

const handlers = [
  http.get("/api/recovery-policy/global/*", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery policy",
      Details: {
        scopeType: "global",
        scopeKey: "*",
        revision: 3,
        overrides: policy,
        inherited: policy,
        effective: policy,
        updatedBy: "dba",
        changeReason: "生产基线",
      },
    }),
  ),
  http.get("/api/recovery-hook-profiles", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery hook profiles",
      Details: [
        {
          id: "notify-dba",
          name: "通知 DBA",
          commands: ["/opt/hooks/notify-dba"],
          timeoutSeconds: 30,
          failurePolicy: "continue",
          outputLimitBytes: 65536,
          enabled: true,
          revision: 2,
        },
      ],
    }),
  ),
  http.get("/api/recovery-hook-assignments/global/*", () =>
    HttpResponse.json({
      Code: "OK",
      Message: "Recovery hook assignments",
      Details: [
        {
          scopeType: "global",
          scopeKey: "*",
          phase: "post_failover",
          mode: "replace",
          profileIds: ["notify-dba"],
          revision: 1,
        },
      ],
    }),
  ),
];

const meta = {
  title: "控制台/恢复配置",
  component: RecoverySettingsPage,
  parameters: { layout: "fullscreen", msw: { handlers } },
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
    ).toHaveValue(30);
    await expect(
      modal.getByRole("spinbutton", { name: "输出上限" }),
    ).toHaveValue(65536);
    await expect(modal.getByText("秒")).toBeVisible();
    await expect(modal.getByText("字节")).toBeVisible();
  },
};
