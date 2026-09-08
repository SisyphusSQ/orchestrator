import dagre from "@dagrejs/dagre";
import type { Instance } from "../api/types";
import { instanceID, instanceState, isReplica } from "./instance";

export type MoveMode = "smart" | "classic" | "gtid" | "pseudo-gtid";
export function visibleInstances(
  instances: Instance[],
  collapsed: Set<string>,
) {
  const hidden = new Set<string>();
  for (const root of collapsed) {
    const visited = new Set([root]);
    const queue = [root];
    while (queue.length) {
      const parent = queue.shift()!;
      for (const child of instances.filter(
        (item) => instanceID(item.MasterKey) === parent,
      )) {
        const id = instanceID(child.Key);
        if (visited.has(id) || child.IsCoMaster) continue;
        visited.add(id);
        hidden.add(id);
        queue.push(id);
      }
    }
  }
  return instances.filter((item) => !hidden.has(instanceID(item.Key)));
}
export function layoutInstances(instances: Instance[], compact: boolean) {
  const graph = new dagre.graphlib.Graph();
  graph.setGraph({
    rankdir: "TB",
    nodesep: 36,
    ranksep: compact ? 65 : 95,
    marginx: 25,
    marginy: 20,
  });
  graph.setDefaultEdgeLabel(() => ({}));
  const ids = new Set(instances.map((instance) => instanceID(instance.Key)));
  for (const instance of instances)
    graph.setNode(instanceID(instance.Key), {
      width: 270,
      height: compact ? 100 : 156,
    });
  for (const instance of instances)
    if (
      ids.has(instanceID(instance.MasterKey)) &&
      instanceID(instance.MasterKey) !== instanceID(instance.Key)
    )
      graph.setEdge(instanceID(instance.MasterKey), instanceID(instance.Key));
  dagre.layout(graph);
  return new Map(
    instances.map((instance) => {
      const node = graph.node(instanceID(instance.Key));
      return [
        instanceID(instance.Key),
        { x: node.x - 135, y: node.y - (compact ? 50 : 78) },
      ];
    }),
  );
}
export function moveDecision(
  source: Instance,
  target: Instance,
  instances: Instance[],
  mode: MoveMode,
): { action?: string; useTargetAsSource?: boolean; reason?: string } {
  if (instanceID(source.Key) === instanceID(target.Key))
    return { reason: "不能选择实例自身" };
  if (
    !source.IsLastCheckValid ||
    !target.IsLastCheckValid ||
    !source.IsRecentlyChecked ||
    !target.IsRecentlyChecked
  )
    return { reason: "实例状态不可用或信息过期，请先探测" };
  if (
    !target.LogBinEnabled ||
    (isReplica(target) && !target.LogReplicationUpdatesEnabled)
  )
    return { reason: "目标需要开启 Binlog 和复制更新日志" };
  if (
    instanceState(source).severity >= 2 ||
    instanceState(target).severity >= 2
  )
    return { reason: "复制状态异常或延迟未知，请先检查实例详情" };
  if (
    !isReplica(source) &&
    instanceID(target.MasterKey) === instanceID(source.Key)
  )
    return { action: "make-co-master", useTargetAsSource: true };
  let ancestor: Instance | undefined = target;
  const seen = new Set<string>();
  while (ancestor && !seen.has(instanceID(ancestor.Key))) {
    if (instanceID(ancestor.Key) === instanceID(source.Key))
      return { reason: "此调整会形成复制环路" };
    seen.add(instanceID(ancestor.Key));
    ancestor = instances.find(
      (item) => instanceID(item.Key) === instanceID(ancestor!.MasterKey),
    );
  }
  if (instanceID(source.MasterKey) === instanceID(target.Key))
    return { reason: "目标已经是当前上游" };
  if (mode === "classic") {
    if (
      instanceID(source.MasterKey) === instanceID(target.MasterKey) &&
      isReplica(source)
    )
      return { action: "move-below" };
    const parent = instances.find(
      (item) => instanceID(item.Key) === instanceID(source.MasterKey),
    );
    if (parent && instanceID(parent.MasterKey) === instanceID(target.Key))
      return { action: "move-up" };
    return {
      reason: "按位点调整需要选择同级副本或上游的上游；其他关系可使用智能模式",
    };
  }
  return {
    action:
      mode === "gtid"
        ? "move-below-gtid"
        : mode === "pseudo-gtid"
          ? "match-below"
          : "relocate",
  };
}
