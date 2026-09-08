import { endpoint } from "../api/client";
import type { InstanceKey } from "../api/types";

export type Values = Record<string, string | number | undefined>;
export interface ActionContext {
  instance?: InstanceKey;
  cluster?: string;
  recovery?: string | number;
  seed?: string | number;
  agent?: string;
}
export interface ActionField {
  name: string;
  label: string;
  type?: "number" | "textarea" | "select";
  required?: boolean;
  initial?: string | number;
  options?: { value: string; label: string }[];
}
export interface ActionDefinition {
  id: string;
  label: string;
  group: string;
  description: string;
  fields: ActionField[];
  path: (context: ActionContext, values: Values) => string;
}
const target: ActionField[] = [
  { name: "targetHost", label: "目标主机", required: true },
  {
    name: "targetPort",
    label: "目标端口",
    type: "number",
    required: true,
    initial: 3306,
  },
];
const reason: ActionField[] = [
  { name: "owner", label: "负责人", required: true },
  { name: "reason", label: "原因", type: "textarea", required: true },
];
const duration: ActionField = {
  name: "duration",
  label: "持续时间",
  required: true,
  initial: "1h",
};
const fields = (name: string, label: string): ActionField[] => [
  { name, label, required: true },
];
function instanceParts(context: ActionContext): [string, number] {
  if (!context.instance) throw new Error("请选择实例");
  return [context.instance.Hostname, context.instance.Port];
}
const definitions: ActionDefinition[] = [];
function instanceAction(
  id: string,
  label: string,
  group: string,
  withTarget = false,
  description = "操作由服务端校验并执行，完成后将重新读取实例和集群状态。",
) {
  definitions.push({
    id,
    label,
    group,
    description,
    fields: withTarget ? target : [],
    path: (context, values) =>
      endpoint(
        id,
        ...instanceParts(context),
        ...(withTarget
          ? [String(values.targetHost), Number(values.targetPort)]
          : []),
      ),
  });
}
for (const [id, label] of [
  ["refresh", "立即探测"],
  ["forget", "遗忘实例"],
  ["start-replica", "启动复制"],
  ["stop-replica", "停止复制"],
  ["restart-replica", "重启复制"],
  ["detach-replica", "分离复制"],
  ["reattach-replica", "恢复分离的复制"],
  ["reattach-replica-master-host", "恢复上游主机"],
  ["set-read-only", "设为只读"],
  ["set-writeable", "设为可写"],
  ["enable-gtid", "启用 GTID"],
  ["disable-gtid", "停用 GTID"],
  ["end-maintenance", "结束维护"],
  ["end-downtime", "结束停机"],
])
  instanceAction(id, label, "实例管理");
for (const [id, label] of [
  ["reset-replica", "重置复制"],
  ["skip-query", "跳过复制错误"],
  ["gtid-errant-reset-master", "重置异常 GTID"],
  ["gtid-errant-inject-empty", "注入空 GTID 事务"],
])
  instanceAction(
    id,
    label,
    "高级操作",
    false,
    "此操作会改变复制状态或事务历史。请核实当前错误、复制位点和业务影响，再明确确认执行。",
  );
for (const [id, label] of [
  ["relocate", "智能迁移副本"],
  ["relocate-replicas", "批量智能迁移下游"],
  ["move-below", "按位点迁移副本"],
  ["move-below-gtid", "按 GTID 迁移副本"],
  ["move-replicas-gtid", "按 GTID 迁移下游"],
  ["match-below", "按 Pseudo-GTID 匹配副本"],
  ["match-replicas", "按 Pseudo-GTID 匹配下游"],
  ["move-equivalent", "迁移到等价位点"],
])
  instanceAction(
    id,
    label,
    "拓扑调整",
    true,
    "将改变复制关系。迁移可能包含多个步骤；失败后必须以回读的实际拓扑为准。",
  );
instanceAction("make-co-master", "与当前主库建立双主", "拓扑调整");
for (const [id, label] of [
  ["move-up", "提升一级"],
  ["move-up-replicas", "下游提升一级"],
  ["repoint", "重新指向上游"],
  ["repoint-replicas", "下游重新指向"],
  ["regroup-replicas", "重组下游副本"],
  ["take-master", "与主库交换角色"],
  ["take-siblings", "接管同级副本"],
  ["make-master", "提升为主库"],
  ["make-local-master", "提升为局部主库"],
])
  instanceAction(id, label, "拓扑调整");
for (const [id, label] of [
  ["recover", "执行故障恢复"],
  ["recover-lite", "执行轻量恢复"],
  ["force-master-failover", "强制主库故障转移"],
  ["graceful-master-takeover", "平滑主库切换"],
])
  instanceAction(
    id,
    label,
    "恢复与切换",
    false,
    "将触发恢复或主库切换，可能影响写入和复制关系。请核实当前主库、候选副本与集群状态。",
  );
definitions.push({
  id: "recover-designated",
  label: "指定副本执行恢复",
  group: "恢复与切换",
  description: "使用指定副本作为候选主库执行故障恢复。",
  fields: target,
  path: (c, v) =>
    endpoint(
      "recover",
      ...instanceParts(c),
      String(v.targetHost),
      Number(v.targetPort),
    ),
});
definitions.push({
  id: "graceful-designated",
  label: "切换到指定副本",
  group: "恢复与切换",
  description: "将主库平滑切换到指定副本。",
  fields: target,
  path: (c, v) =>
    endpoint(
      "graceful-master-takeover",
      ...instanceParts(c),
      String(v.targetHost),
      Number(v.targetPort),
    ),
});
for (const [id, label] of [
  ["begin-maintenance", "开始维护"],
  ["begin-downtime", "开始停机"],
])
  definitions.push({
    id,
    label,
    group: "实例管理",
    description: "记录负责人、原因和期限。维护与停机具有不同的恢复处理语义。",
    fields: [...reason, duration],
    path: (c, v) =>
      endpoint(
        id,
        ...instanceParts(c),
        String(v.owner),
        String(v.reason),
        ...(id === "begin-downtime" ? [String(v.duration)] : []),
      ) +
      (id === "begin-maintenance"
        ? `?duration=${encodeURIComponent(String(v.duration))}`
        : ""),
  });
definitions.push({
  id: "register-candidate",
  label: "设置晋升规则",
  group: "恢复与切换",
  description: "调整此实例作为候选主库的优先级。",
  fields: [
    {
      name: "rule",
      label: "晋升规则",
      type: "select",
      required: true,
      initial: "neutral",
      options: ["must", "prefer", "neutral", "prefer_not", "must_not"].map(
        (value) => ({ value, label: value }),
      ),
    },
  ],
  path: (c, v) =>
    endpoint("register-candidate", ...instanceParts(c), String(v.rule)),
});
definitions.push({
  id: "discover",
  label: "发现实例",
  group: "发现",
  description:
    "从此实例开始发现复制拓扑，服务端会连接该 MySQL 实例并探查上下游。",
  fields: target,
  path: (_, v) =>
    endpoint("discover", String(v.targetHost), Number(v.targetPort)),
});
definitions.push({
  id: "set-cluster-alias",
  label: "修改集群别名",
  group: "集群",
  description: "设置便于识别的集群名称。",
  fields: fields("alias", "集群别名"),
  path: (c, v) =>
    endpoint("set-cluster-alias", c.cluster!) +
    `?alias=${encodeURIComponent(String(v.alias))}`,
});
definitions.push({
  id: "ack-recovery",
  label: "确认恢复记录",
  group: "审计",
  description: "确认已查看并处理本次恢复结果，保留确认原因。",
  fields: fields("reason", "确认原因"),
  path: (c, v) =>
    endpoint("ack-recovery", c.recovery!) +
    `?comment=${encodeURIComponent(String(v.reason))}`,
});
definitions.push({
  id: "ack-cluster",
  label: "确认集群恢复记录",
  group: "集群",
  description: "确认此集群已有的恢复记录。",
  fields: fields("reason", "确认原因"),
  path: (c, v) =>
    endpoint("ack-recovery", "cluster", c.cluster!) +
    `?comment=${encodeURIComponent(String(v.reason))}`,
});
definitions.push({
  id: "submit-pool-instances",
  label: "更新资源池",
  group: "资源池",
  description: "此操作替换整个资源池的成员列表，清空列表表示清空资源池。",
  fields: [
    ...fields("pool", "资源池名称"),
    {
      name: "instances",
      label: "成员（host:port，以逗号分隔）",
      type: "textarea",
    },
  ],
  path: (_, v) =>
    endpoint("submit-pool-instances", String(v.pool)) +
    `?instances=${encodeURIComponent(String(v.instances ?? ""))}`,
});
for (const [id, label] of [
  ["enable-global-recoveries", "启用自动恢复"],
  ["disable-global-recoveries", "暂停自动恢复"],
  ["reload-configuration", "重新加载配置"],
  ["reset-hostname-resolve-cache", "清空主机解析缓存"],
  ["reelect", "重新选举活动节点"],
])
  definitions.push({
    id,
    label,
    group: "系统",
    description: "此操作影响整个 Orchestrator 服务，请确认维护安排。",
    fields: [],
    path: () => endpoint(id),
  });
for (const [id, label] of [
  ["agent-umount", "卸载数据卷"],
  ["agent-create-snapshot", "创建快照"],
  ["agent-mysql-start", "启动 MySQL"],
  ["agent-mysql-stop", "停止 MySQL"],
])
  definitions.push({
    id,
    label,
    group: "Agent",
    description: "操作将在选中的 Agent 主机上执行。",
    fields: [],
    path: (c) => endpoint(id, c.agent!),
  });
for (const [id, label] of [
  ["agent-mount", "挂载逻辑卷"],
  ["agent-removelv", "删除逻辑卷"],
])
  definitions.push({
    id,
    label,
    group: "Agent",
    description: "操作会改变 Agent 主机的数据卷状态，请核实目标卷及业务影响。",
    fields: fields("lv", "逻辑卷名称"),
    path: (c, v) =>
      endpoint(id, c.agent!) + `?lv=${encodeURIComponent(String(v.lv))}`,
  });
definitions.push({
  id: "agent-seed",
  label: "从快照恢复实例",
  group: "Agent",
  description:
    "将使用源主机的快照恢复目标 Agent。请确认目标 MySQL 已停止且目标数据可被替换。",
  fields: fields("source", "快照来源主机"),
  path: (c, v) => endpoint("agent-seed", c.agent!, String(v.source)),
});
definitions.push({
  id: "agent-abort-seed",
  label: "中止恢复任务",
  group: "Seed",
  description: "中止此数据恢复任务。已执行的步骤不会自动撤销。",
  fields: [],
  path: (c) => endpoint("agent-abort-seed", c.seed!),
});
for (const action of definitions) {
  if (
    [
      "relocate-replicas",
      "move-up-replicas",
      "repoint-replicas",
      "move-replicas-gtid",
      "match-replicas",
    ].includes(action.id)
  ) {
    action.fields = [
      ...action.fields,
      { name: "pattern", label: "仅匹配下游（正则表达式，可留空）" },
    ];
    const path = action.path;
    action.path = (context, values) =>
      path(context, values) +
      (values.pattern
        ? `?pattern=${encodeURIComponent(String(values.pattern))}`
        : "");
  }
}
export const actions = definitions;
export function getAction(id: string): ActionDefinition {
  const action = actions.find((item) => item.id === id);
  if (!action) throw new Error(`未知操作：${id}`);
  return action;
}
