import { useEffect, useMemo, useState } from "react";
import {
  Alert,
  App,
  Button,
  Card,
  Divider,
  Flex,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  InfoCircleOutlined,
  PlusOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { endpoint, executeJSON } from "../api/client";
import type {
  HookAssignment,
  HookProfile,
  RecoveryPolicy,
  RecoveryPolicyDocument,
} from "../api/types";
import { useQuery } from "../api/use-query";
import { useConfig } from "../app/context";
import { QueryError } from "../components/common";

type PolicyKey = keyof RecoveryPolicy;
type Setting = {
  key: PolicyKey;
  label: string;
  hint: string;
  kind: "boolean" | "number" | "filters" | "sql-policy";
  unit?: string;
};

export const policySettings: Setting[] = [
  {
    key: "autoMasterRecovery",
    label: "自动主库恢复",
    kind: "boolean",
    hint: "检测到主库故障后是否允许自动执行恢复。默认关闭，建议完成演练后按集群开启。",
  },
  {
    key: "autoIntermediateMasterRecovery",
    label: "自动中间主库恢复",
    kind: "boolean",
    hint: "检测到中间层主库故障后是否自动重组其下游。默认关闭。",
  },
  {
    key: "recoveryIgnoreHostnameFilters",
    label: "恢复忽略主机",
    kind: "filters",
    hint: "匹配这些正则表达式的主机不会触发自动恢复；每行一个正则。默认空。",
  },
  {
    key: "promotionIgnoreHostnameFilters",
    label: "提升忽略主机",
    kind: "filters",
    hint: "匹配这些正则表达式的实例不会被选作提升候选；每行一个正则。默认空。",
  },
  {
    key: "problemIgnoreHostnameFilters",
    label: "问题展示忽略主机",
    kind: "filters",
    hint: "匹配这些正则表达式的实例不会出现在问题实例列表；每行一个正则。默认空。",
  },
  {
    key: "failureDetectionPeriodBlockMinutes",
    label: "故障检测阻塞窗口",
    kind: "number",
    unit: "分钟",
    hint: "同一故障检测在该窗口内不会重复登记。默认 60 分钟。",
  },
  {
    key: "recoveryPeriodBlockSeconds",
    label: "恢复阻塞窗口",
    kind: "number",
    unit: "秒",
    hint: "同一实例或集群完成恢复后，在该窗口内阻止再次恢复，避免抖动。默认 3600 秒。",
  },
  {
    key: "reasonableReplicationLagSeconds",
    label: "合理复制延迟",
    kind: "number",
    unit: "秒",
    hint: "超过该值的复制延迟会被视为异常。默认 10 秒。",
  },
  {
    key: "reasonableMaintenanceReplicationLagSeconds",
    label: "维护操作延迟上限",
    kind: "number",
    unit: "秒",
    hint: "拓扑维护操作允许的复制延迟上限。默认 20 秒。",
  },
  {
    key: "verifyReplicationFilters",
    label: "校验复制过滤规则",
    kind: "boolean",
    hint: "候选实例存在不兼容复制过滤规则时阻止拓扑变更。默认关闭。",
  },
  {
    key: "failMasterPromotionOnLagMinutes",
    label: "提升失败延迟阈值",
    kind: "number",
    unit: "分钟",
    hint: "候选延迟达到该值时终止主库提升；0 表示不启用。默认 0。",
  },
  {
    key: "sqlThreadPromotionPolicy",
    label: "SQL 线程未追平策略",
    kind: "sql-policy",
    hint: "allow 直接提升；wait 等待 SQL 线程追平；reject 终止提升。默认 allow。",
  },
  {
    key: "recoverNonWriteableMaster",
    label: "恢复只读主库",
    kind: "boolean",
    hint: "主库可达但意外只读时，自动尝试恢复为可写。默认关闭。",
  },
  {
    key: "coMasterRecoveryMustPromoteOtherCoMaster",
    label: "双主必须提升另一主库",
    kind: "boolean",
    hint: "双主故障恢复时强制选择另一台主库。默认开启。",
  },
  {
    key: "detachLostReplicasAfterMasterFailover",
    label: "故障后分离丢失副本",
    kind: "boolean",
    hint: "主库切换后对无法跟随新主库的副本执行可逆分离。默认开启。",
  },
  {
    key: "applyMySQLPromotionAfterMasterFailover",
    label: "应用 MySQL 提升动作",
    kind: "boolean",
    hint: "提升后执行 RESET REPLICA 与关闭 read_only 等 MySQL 动作。默认开启，关闭可能留下未完成的主库状态。",
  },
  {
    key: "preventCrossDataCenterMasterFailover",
    label: "禁止跨机房提升",
    kind: "boolean",
    hint: "候选实例与故障主库不在同一机房时拒绝提升。默认关闭。",
  },
  {
    key: "preventCrossRegionMasterFailover",
    label: "禁止跨地域提升",
    kind: "boolean",
    hint: "候选实例与故障主库不在同一地域时拒绝提升。默认关闭。",
  },
  {
    key: "masterFailoverDetachReplicaMasterHost",
    label: "提升后分离旧上游",
    kind: "boolean",
    hint: "主库切换后清除提升实例记录的旧上游主机。默认关闭。",
  },
  {
    key: "postponeReplicaRecoveryOnLagMinutes",
    label: "延迟副本恢复推迟阈值",
    kind: "number",
    unit: "分钟",
    hint: "SQL_DELAY 超过该值时把副本重组放到延后队列；0 表示不启用。默认 0。",
  },
  {
    key: "enforceExactSemiSyncReplicas",
    label: "严格半同步副本数",
    kind: "boolean",
    hint: "自动调整半同步副本，使启用数量精确满足主库期望。默认关闭。",
  },
  {
    key: "recoverLockedSemiSyncMaster",
    label: "恢复半同步锁定主库",
    kind: "boolean",
    hint: "主库因半同步确认不足而锁定时自动调整副本。默认关闭。",
  },
  {
    key: "reasonableLockedSemiSyncMasterSeconds",
    label: "半同步锁定判断时间",
    kind: "number",
    unit: "秒",
    hint: "持续达到该时间才确认半同步主库锁定；0 表示跟随“合理复制延迟”。默认 0。",
  },
];

export const hookPhases = [
  [
    "failure_detection",
    "故障检测后",
    "完成故障检测登记后执行；abort 会阻止本次处理。",
  ],
  [
    "pre_failover",
    "故障切换前",
    "任何实际拓扑恢复动作前执行，常用于外部互锁。",
  ],
  ["post_master_failover", "主库切换后", "主库提升结果确定后执行。"],
  [
    "post_intermediate_master_failover",
    "中间主库切换后",
    "中间层主库恢复完成后执行。",
  ],
  ["post_failover", "恢复结束后", "所有恢复类型结束后执行。"],
  ["post_unsuccessful_failover", "恢复失败后", "恢复未成功时执行。"],
  ["pre_graceful_takeover", "优雅切换前", "原主库设为只读之前执行。"],
  ["post_graceful_takeover", "优雅切换后", "优雅切换完成后执行。"],
  ["post_take_master", "Take Master 后", "手工 Take Master 成功后执行。"],
] as const;

function ScopePicker({
  scope,
  setScope,
  alias,
  setAlias,
}: {
  scope: "global" | "cluster";
  setScope: (value: "global" | "cluster") => void;
  alias: string;
  setAlias: (value: string) => void;
}) {
  return (
    <Flex gap={12} wrap align="center">
      <Select
        aria-label="配置作用域"
        value={scope}
        onChange={setScope}
        options={[
          { value: "global", label: "全局默认" },
          { value: "cluster", label: "集群覆盖" },
        ]}
        style={{ width: 150 }}
      />
      {scope === "cluster" && (
        <Input
          aria-label="显式集群别名"
          value={alias}
          onChange={(event) => setAlias(event.target.value)}
          placeholder="输入显式且唯一的集群别名"
          style={{ width: 320 }}
        />
      )}
      <Typography.Text type="secondary">
        优先级：集群覆盖 &gt; 全局配置 &gt; 代码默认值
      </Typography.Text>
    </Flex>
  );
}

function SettingControl({
  setting,
  value,
  disabled,
  onChange,
}: {
  setting: Setting;
  value: RecoveryPolicy[PolicyKey];
  disabled: boolean;
  onChange: (value: RecoveryPolicy[PolicyKey]) => void;
}) {
  if (setting.kind === "boolean")
    return (
      <Switch
        checked={Boolean(value)}
        disabled={disabled}
        onChange={onChange}
      />
    );
  if (setting.kind === "number")
    return (
      <InputNumber
        min={0}
        value={Number(value)}
        disabled={disabled}
        suffix={setting.unit}
        style={{ width: 180 }}
        onChange={(next) => onChange(Number(next ?? 0))}
      />
    );
  if (setting.kind === "sql-policy")
    return (
      <Select
        value={String(value)}
        disabled={disabled}
        style={{ width: 180 }}
        onChange={(next) => onChange(next as RecoveryPolicy[PolicyKey])}
        options={[
          { value: "allow", label: "allow · 允许" },
          { value: "wait", label: "wait · 等待追平" },
          { value: "reject", label: "reject · 拒绝提升" },
        ]}
      />
    );
  return (
    <Input.TextArea
      value={(value as string[]).join("\n")}
      disabled={disabled}
      autoSize={{ minRows: 2, maxRows: 5 }}
      placeholder="每行一个正则表达式"
      onChange={(event) =>
        onChange(
          event.target.value
            .split("\n")
            .map((item) => item.trim())
            .filter(Boolean),
        )
      }
    />
  );
}

export function PolicyPanel() {
  const { message } = App.useApp();
  const config = useConfig();
  const [scope, setScope] = useState<"global" | "cluster">("global");
  const [alias, setAlias] = useState("");
  const scopeKey = scope === "global" ? "*" : alias.trim();
  const state = useQuery<RecoveryPolicyDocument>(
    scopeKey ? endpoint("recovery-policy", scope, scopeKey) : null,
    0,
  );
  const [values, setValues] = useState<RecoveryPolicy>();
  const [overrides, setOverrides] = useState<Partial<RecoveryPolicy>>({});
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    if (state.data) {
      setValues(state.data.effective);
      setOverrides(state.data.overrides);
    }
  }, [state.data]);
  const save = async () => {
    if (!state.data || !values || !reason.trim()) {
      void message.warning("请填写变更原因");
      return;
    }
    setSaving(true);
    try {
      await executeJSON("/recovery-policy", {
        scopeType: scope,
        scopeKey,
        expectedRevision: state.data.revision,
        overrides,
        changeReason: reason.trim(),
      });
      void message.success("恢复策略已保存并通过 Raft 同步");
      setReason("");
      state.refresh();
    } catch (error) {
      void message.error((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <ScopePicker
        scope={scope}
        setScope={setScope}
        alias={alias}
        setAlias={setAlias}
      />
      {scope === "cluster" && !scopeKey && (
        <Alert showIcon type="info" title="输入显式集群别名后加载覆盖配置" />
      )}
      <QueryError error={state.error} retry={state.refresh} />
      {state.loading && !state.data ? (
        <Spin />
      ) : (
        values && (
          <>
            <Table
              pagination={false}
              rowKey="key"
              dataSource={policySettings}
              columns={[
                {
                  title: "配置项",
                  width: 285,
                  render: (_, item: Setting) => (
                    <Space>
                      <span>{item.label}</span>
                      <Tooltip title={item.hint}>
                        <InfoCircleOutlined aria-label={`${item.label}说明`} />
                      </Tooltip>
                    </Space>
                  ),
                },
                {
                  title: scope === "cluster" ? "覆盖" : "自定义",
                  width: 85,
                  render: (_: unknown, item: Setting) => (
                    <Switch
                      size="small"
                      checked={Object.hasOwn(overrides, item.key)}
                      onChange={(checked) =>
                        setOverrides((old) => {
                          const next = { ...old };
                          if (checked) {
                            next[item.key] = values[item.key] as never;
                          } else {
                            delete next[item.key];
                            setValues((current) => ({
                              ...current!,
                              [item.key]: state.data!.inherited[item.key],
                            }));
                          }
                          return next;
                        })
                      }
                    />
                  ),
                },
                {
                  title: "值",
                  render: (_: unknown, item: Setting) => (
                    <SettingControl
                      setting={item}
                      value={values[item.key]}
                      disabled={!Object.hasOwn(overrides, item.key)}
                      onChange={(next) => {
                        setValues((old) => ({ ...old!, [item.key]: next }));
                        setOverrides((old) => ({ ...old, [item.key]: next }));
                      }}
                    />
                  ),
                },
                {
                  title: "来源",
                  width: 110,
                  render: (_: unknown, item: Setting) =>
                    !Object.hasOwn(overrides, item.key) ? (
                      <Tag>{scope === "cluster" ? "继承全局" : "代码默认"}</Tag>
                    ) : (
                      <Tag color="blue">
                        {scope === "global" ? "全局" : "集群"}
                      </Tag>
                    ),
                },
              ]}
            />
            <Card size="small" title="提交变更">
              <Flex gap={12} align="center">
                <Input
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="必填：说明修改原因，写入审计信息"
                  maxLength={512}
                />
                <Button
                  type="primary"
                  icon={<SaveOutlined />}
                  loading={saving}
                  disabled={!config.authorizedForConfiguration || !scopeKey}
                  onClick={save}
                >
                  保存策略
                </Button>
              </Flex>
              {!config.authorizedForConfiguration && (
                <Typography.Text type="warning">
                  当前账号没有配置管理员权限。
                </Typography.Text>
              )}
            </Card>
          </>
        )
      )}
    </Space>
  );
}

const emptyProfile = (): HookProfile => ({
  id: "",
  name: "",
  commands: [""],
  timeoutSeconds: 30,
  failurePolicy: "abort",
  outputLimitBytes: 65536,
  enabled: true,
  revision: 0,
  changeReason: "",
});

export function HooksPanel() {
  const { message, modal } = App.useApp();
  const config = useConfig();
  const profilesState = useQuery<HookProfile[]>("/recovery-hook-profiles", 0);
  const [scope, setScope] = useState<"global" | "cluster">("global");
  const [alias, setAlias] = useState("");
  const scopeKey = scope === "global" ? "*" : alias.trim();
  const assignmentsState = useQuery<HookAssignment[]>(
    scopeKey ? endpoint("recovery-hook-assignments", scope, scopeKey) : null,
    0,
  );
  const [assignments, setAssignments] = useState<
    Record<string, HookAssignment>
  >({});
  const [reason, setReason] = useState("");
  const [editing, setEditing] = useState<HookProfile | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(() => {
    const next: Record<string, HookAssignment> = {};
    for (const [phase] of hookPhases)
      next[phase] = {
        scopeType: scope,
        scopeKey,
        phase,
        mode: "inherit",
        profileIds: [],
        revision: 0,
      };
    for (const item of assignmentsState.data || []) next[item.phase] = item;
    setAssignments(next);
  }, [assignmentsState.data, scope, scopeKey]);
  const profiles = profilesState.data || [];
  const saveProfile = async () => {
    if (
      !editing?.id.trim() ||
      !editing.name.trim() ||
      !editing.commands.some((command) => command.trim()) ||
      !editing.changeReason?.trim()
    ) {
      void message.warning("请完整填写标识、名称、至少一条命令和变更原因");
      return;
    }
    setSaving(true);
    try {
      await executeJSON("/recovery-hook-profiles", {
        profile: {
          ...editing,
          commands: editing.commands.map((item) => item.trim()).filter(Boolean),
        },
        expectedRevision: editing.revision,
      });
      void message.success("Hook 配置已保存");
      setEditing(null);
      profilesState.refresh();
    } catch (error) {
      void message.error((error as Error).message);
    } finally {
      setSaving(false);
    }
  };
  const testProfile = (profile: HookProfile) =>
    modal.confirm({
      title: `执行测试：${profile.name}`,
      content:
        "测试会在服务端以 Orchestrator 进程身份真实执行命令，并写入操作审计。确认当前命令可以安全执行。",
      okText: "确认执行",
      okButtonProps: { danger: true },
      async onOk() {
        const response = await executeJSON<
          { output: string; error?: string }[]
        >("/recovery-hook-test", { profileId: profile.id });
        const failed = response.Details.find((item) => item.error);
        if (failed) throw new Error(failed.error);
        void message.success(`测试完成：${response.Details.length} 条命令`);
      },
    });
  const saveAssignment = async (assignment: HookAssignment) => {
    if (!reason.trim()) {
      void message.warning("请先填写分配变更原因");
      return;
    }
    try {
      await executeJSON("/recovery-hook-assignments", {
        assignment: {
          ...assignment,
          scopeType: scope,
          scopeKey,
          changeReason: reason.trim(),
        },
        expectedRevision: assignment.revision,
      });
      void message.success("Hook 分配已保存");
      assignmentsState.refresh();
    } catch (error) {
      void message.error((error as Error).message);
    }
  };
  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <Alert
        showIcon
        type="warning"
        title="Hook 命令以 Orchestrator 进程身份执行"
        description="仅配置管理员可修改。命令按配置顺序执行，每条命令都有超时和输出上限；不要在命令文本中写入密钥。"
      />
      <Card
        title="Hook 配置库"
        extra={
          <Button
            icon={<PlusOutlined />}
            disabled={!config.authorizedForConfiguration}
            onClick={() => setEditing(emptyProfile())}
          >
            新建配置
          </Button>
        }
      >
        <Table
          size="small"
          pagination={false}
          rowKey="id"
          dataSource={profiles}
          columns={[
            { title: "名称", dataIndex: "name" },
            {
              title: "命令数",
              render: (_, row: HookProfile) => row.commands.length,
            },
            {
              title: "超时",
              render: (_, row: HookProfile) => `${row.timeoutSeconds} 秒`,
            },
            { title: "失败策略", dataIndex: "failurePolicy" },
            {
              title: "状态",
              render: (_, row: HookProfile) => (
                <Tag color={row.enabled ? "green" : "default"}>
                  {row.enabled ? "启用" : "停用"}
                </Tag>
              ),
            },
            {
              title: "操作",
              render: (_, row: HookProfile) => (
                <Space>
                  <Button
                    type="link"
                    disabled={!config.authorizedForConfiguration}
                    onClick={() => setEditing({ ...row })}
                  >
                    编辑
                  </Button>
                  <Button
                    type="link"
                    danger
                    disabled={
                      !config.authorizedForConfiguration || !row.enabled
                    }
                    onClick={() => testProfile(row)}
                  >
                    测试执行
                  </Button>
                </Space>
              ),
            },
          ]}
        />
      </Card>
      <Divider />
      <ScopePicker
        scope={scope}
        setScope={setScope}
        alias={alias}
        setAlias={setAlias}
      />
      <Card
        title="生命周期分配"
        extra={
          <Tooltip title="集群 inherit 使用全局配置；replace 完整替换；disable 显式关闭。不会隐式追加。">
            <InfoCircleOutlined />
          </Tooltip>
        }
      >
        <Flex gap={12} style={{ marginBottom: 16 }}>
          <Input
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            placeholder="必填：分配变更原因"
          />
        </Flex>
        <Table
          pagination={false}
          rowKey={(row) => row[0]}
          dataSource={[...hookPhases]}
          columns={[
            {
              title: "阶段",
              width: 220,
              render: (_, row) => (
                <Space>
                  <span>{row[1]}</span>
                  <Tooltip title={row[2]}>
                    <InfoCircleOutlined />
                  </Tooltip>
                </Space>
              ),
            },
            {
              title: "覆盖方式",
              width: 180,
              render: (_, row) => (
                <Select
                  value={assignments[row[0]]?.mode || "inherit"}
                  style={{ width: 145 }}
                  onChange={(mode) =>
                    setAssignments((old) => ({
                      ...old,
                      [row[0]]: {
                        ...old[row[0]],
                        mode,
                        profileIds:
                          mode === "replace"
                            ? old[row[0]]?.profileIds || []
                            : [],
                      },
                    }))
                  }
                  options={[
                    { value: "inherit", label: "inherit · 继承" },
                    { value: "replace", label: "replace · 替换" },
                    { value: "disable", label: "disable · 关闭" },
                  ]}
                />
              ),
            },
            {
              title: "Hook 配置",
              render: (_, row) => (
                <Select
                  mode="multiple"
                  disabled={assignments[row[0]]?.mode !== "replace"}
                  value={assignments[row[0]]?.profileIds || []}
                  style={{ width: "100%" }}
                  placeholder="按选择顺序执行"
                  onChange={(profileIds) =>
                    setAssignments((old) => ({
                      ...old,
                      [row[0]]: { ...old[row[0]], profileIds },
                    }))
                  }
                  options={profiles
                    .filter((item) => item.enabled)
                    .map((item) => ({ value: item.id, label: item.name }))}
                />
              ),
            },
            {
              title: "操作",
              width: 100,
              render: (_, row) => (
                <Button
                  type="link"
                  disabled={!config.authorizedForConfiguration || !scopeKey}
                  onClick={() => void saveAssignment(assignments[row[0]])}
                >
                  保存
                </Button>
              ),
            },
          ]}
        />
      </Card>
      <Modal
        title={editing?.revision ? "编辑 Hook 配置" : "新建 Hook 配置"}
        open={!!editing}
        confirmLoading={saving}
        onCancel={() => setEditing(null)}
        onOk={() => void saveProfile()}
        okText="保存"
        destroyOnHidden
      >
        {editing && (
          <Space direction="vertical" style={{ width: "100%" }}>
            <Typography.Text>
              稳定标识{" "}
              <Tooltip title="创建后不可更换；用于分配关系与恢复审计。">
                <InfoCircleOutlined />
              </Tooltip>
            </Typography.Text>
            <Input
              disabled={editing.revision > 0}
              value={editing.id}
              onChange={(event) =>
                setEditing({ ...editing, id: event.target.value })
              }
              placeholder="例如 notify-dba"
            />
            <Typography.Text>显示名称</Typography.Text>
            <Input
              value={editing.name}
              onChange={(event) =>
                setEditing({ ...editing, name: event.target.value })
              }
            />
            <Typography.Text>
              命令{" "}
              <Tooltip title="每行一条，顺序执行；支持现有恢复占位符与 ORC_* 环境变量。">
                <InfoCircleOutlined />
              </Tooltip>
            </Typography.Text>
            <Input.TextArea
              rows={5}
              value={editing.commands.join("\n")}
              onChange={(event) =>
                setEditing({
                  ...editing,
                  commands: event.target.value.split("\n"),
                })
              }
            />
            <Flex gap={12} wrap>
              <Space direction="vertical" size={4}>
                <Typography.Text type="secondary">
                  单命令超时
                </Typography.Text>
                <InputNumber
                  aria-label="单命令超时"
                  min={1}
                  max={3600}
                  value={editing.timeoutSeconds}
                  addonAfter="秒"
                  style={{ width: 180 }}
                  onChange={(value) =>
                    setEditing({ ...editing, timeoutSeconds: Number(value) })
                  }
                />
              </Space>
              <Space direction="vertical" size={4}>
                <Typography.Text type="secondary">
                  输出上限
                </Typography.Text>
                <InputNumber
                  aria-label="输出上限"
                  min={1024}
                  max={1048576}
                  value={editing.outputLimitBytes}
                  addonAfter="字节"
                  style={{ width: 220 }}
                  onChange={(value) =>
                    setEditing({ ...editing, outputLimitBytes: Number(value) })
                  }
                />
              </Space>
              <Space direction="vertical" size={4}>
                <Typography.Text type="secondary">
                  失败策略
                </Typography.Text>
                <Select
                  aria-label="失败策略"
                  value={editing.failurePolicy}
                  style={{ width: 180 }}
                  onChange={(failurePolicy) =>
                    setEditing({ ...editing, failurePolicy })
                  }
                  options={[
                    { value: "abort", label: "失败时中止" },
                    { value: "continue", label: "失败后继续" },
                  ]}
                />
              </Space>
            </Flex>
            <Switch
              checked={editing.enabled}
              checkedChildren="启用"
              unCheckedChildren="停用"
              onChange={(enabled) => setEditing({ ...editing, enabled })}
            />
            <Input
              value={editing.changeReason}
              onChange={(event) =>
                setEditing({ ...editing, changeReason: event.target.value })
              }
              placeholder="必填：变更原因"
            />
          </Space>
        )}
      </Modal>
    </Space>
  );
}

export function RecoverySettingsPage() {
  return (
    <Card
      title="恢复策略与 Hook"
      extra={<Tag color="blue">23 项策略 · 9 个 Hook 阶段</Tag>}
    >
      <Tabs
        items={[
          { key: "policy", label: "恢复策略", children: <PolicyPanel /> },
          { key: "hooks", label: "Pre / Post Hook", children: <HooksPanel /> },
        ]}
      />
    </Card>
  );
}
