import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App, Button, Space, Tag, Tooltip } from "antd";
import {
  ApartmentOutlined,
  DatabaseOutlined,
  DownOutlined,
  RightOutlined,
} from "@ant-design/icons";
import {
  Background,
  Controls,
  MiniMap,
  MarkerType,
  ReactFlow,
  Handle,
  Position,
  applyNodeChanges,
  type Node,
  type NodeProps,
  type Edge,
  type NodeChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { Instance, Maintenance } from "../api/types";
import { useConfig } from "../app/context";
import {
  instanceID,
  instanceState,
  lagText,
  replicationMode,
  role,
} from "../domain/instance";
import {
  layoutInstances,
  moveDecision,
  visibleInstances,
  type MoveMode,
} from "../domain/topology";
import { useOperation } from "./operations";

type InstanceNode = Node<
  {
    instance: Instance;
    label: string;
    compact: boolean;
    collapsed: boolean;
    children: number;
    color?: string;
    poolNames: string[];
    maintenance?: Maintenance;
    inspect: () => void;
    fold: () => void;
  },
  "instance"
>;
const DatabaseNode = memo(function DatabaseNode({
  data,
  selected,
}: NodeProps<InstanceNode>) {
  const state = instanceState(data.instance);
  return (
    <div
      className={`database-node ${selected ? "selected" : ""} severity-${state.severity}`}
      style={{ borderTopColor: data.color }}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <div className="node-heading">
        <span className="node-icon">
          <DatabaseOutlined />
        </span>
        <Button type="text" className="node-name nodrag" onClick={data.inspect}>
          {data.label}
        </Button>
        <Tag color={role(data.instance) === "副本" ? "default" : "blue"}>
          {role(data.instance)}
        </Tag>
      </div>
      <div className="node-status">
        <span
          className={`state-dot ${state.severity === 3 ? "red" : state.severity === 2 ? "orange" : "green"}`}
        />
        {state.label}
        {data.maintenance && (
          <Tooltip
            title={`${data.maintenance.Owner} · ${data.maintenance.Reason}`}
          >
            <Tag>维护中</Tag>
          </Tooltip>
        )}
        <span className="node-lag">{lagText(data.instance)}</span>
      </div>
      {!data.compact && (
        <>
          <div className="node-meta">
            <span>{data.instance.Version}</span>
            <span>{replicationMode(data.instance)}</span>
          </div>
          <div className="node-meta">
            <span>{data.instance.DataCenter || "未标注机房"}</span>
            <span>{data.instance.ReadOnly ? "只读" : "可写"}</span>
          </div>
          {data.poolNames.length > 0 && (
            <div className="node-pools">{data.poolNames.join(" · ")}</div>
          )}
        </>
      )}
      {data.children > 0 && (
        <Tooltip title={data.collapsed ? "展开下游" : "折叠下游"}>
          <Button
            className="node-fold nodrag"
            size="small"
            icon={data.collapsed ? <RightOutlined /> : <DownOutlined />}
            onClick={data.fold}
          >
            {data.children}
          </Button>
        </Tooltip>
      )}
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
});
const nodeTypes = { instance: DatabaseNode };
const colors = ["#1677ff", "#13a8a8", "#722ed1", "#eb2f96", "#d48806"];

export function Topology({
  instances,
  onSelect,
  compact,
  anonymize,
  aliases,
  colorize,
  mode,
  pools,
  maintenance,
}: {
  instances: Instance[];
  onSelect: (instance: Instance) => void;
  compact: boolean;
  anonymize: boolean;
  aliases: boolean;
  colorize: boolean;
  mode: MoveMode;
  pools: Record<string, string[]>;
  maintenance: Maintenance[];
}) {
  const config = useConfig();
  const operate = useOperation();
  const { message } = App.useApp();
  const [collapsed, setCollapsed] = useState(new Set<string>());
  const [nodes, setNodes] = useState<InstanceNode[]>([]);
  const structure = useRef("");
  const visible = useMemo(
    () => visibleInstances(instances, collapsed),
    [instances, collapsed],
  );
  const dc = useMemo(
    () => [...new Set(instances.map((instance) => instance.DataCenter))].sort(),
    [instances],
  );
  useEffect(() => {
    const signature = JSON.stringify([
      visible.map((instance) => [
        instanceID(instance.Key),
        instanceID(instance.MasterKey),
      ]),
      compact,
    ]);
    const rearrange = signature !== structure.current;
    structure.current = signature;
    const positions = rearrange ? layoutInstances(visible, compact) : undefined;
    setNodes((previous) =>
      visible.map((instance) => {
        const id = instanceID(instance.Key);
        const old = previous.find((node) => node.id === id);
        const index =
          instances.findIndex((item) => instanceID(item.Key) === id) + 1;
        return {
          id,
          type: "instance",
          position: positions?.get(id) || old?.position || { x: 0, y: 0 },
          selected: old?.selected,
          data: {
            instance,
            label: anonymize
              ? `实例 ${String(index).padStart(2, "0")}`
              : aliases && instance.InstanceAlias
                ? instance.InstanceAlias
                : id.replace(config.removeTextFromHostnameDisplay, ""),
            compact,
            collapsed: collapsed.has(id),
            children: instances.filter(
              (item) => instanceID(item.MasterKey) === id && !item.IsCoMaster,
            ).length,
            color: colorize
              ? colors[dc.indexOf(instance.DataCenter) % colors.length]
              : undefined,
            poolNames: pools[id] || [],
            maintenance: maintenance.find(
              (item) => instanceID(item.Key) === id,
            ),
            inspect: () => onSelect(instance),
            fold: () =>
              setCollapsed((old) => {
                const next = new Set(old);
                next.has(id) ? next.delete(id) : next.add(id);
                return next;
              }),
          },
        };
      }),
    );
  }, [
    visible,
    instances,
    compact,
    anonymize,
    aliases,
    colorize,
    collapsed,
    onSelect,
    config.removeTextFromHostnameDisplay,
    dc,
    pools,
    maintenance,
  ]);
  const edges: Edge[] = useMemo(
    () =>
      visible
        .filter(
          (instance) =>
            visible.some(
              (parent) =>
                instanceID(parent.Key) === instanceID(instance.MasterKey),
            ) && instanceID(instance.Key) !== instanceID(instance.MasterKey),
        )
        .map((instance) => ({
          id: `${instanceID(instance.MasterKey)}>${instanceID(instance.Key)}`,
          source: instanceID(instance.MasterKey),
          target: instanceID(instance.Key),
          type: "smoothstep",
          animated: instanceState(instance).severity === 0,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#91a4bc" },
          style: {
            stroke:
              instanceState(instance).severity === 3 ? "#ff7875" : "#91a4bc",
            strokeWidth: 1.6,
          },
        })),
    [visible],
  );
  const changeNodes = useCallback(
    (changes: NodeChange<InstanceNode>[]) =>
      setNodes((old) => applyNodeChanges(changes, old)),
    [],
  );
  return (
    <div
      className="topology-canvas"
      style={{
        height: "calc(100dvh - 460px)",
      }}
    >
      <div className="canvas-caption">
        <ApartmentOutlined /> 复制拓扑{" "}
        <span>拖动查看布局 · 将节点拖到目标节点上调整复制关系</span>
      </div>
      <ReactFlow<InstanceNode>
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={changeNodes}
        fitView
        fitViewOptions={{ padding: 0.22, maxZoom: 1 }}
        minZoom={0.15}
        maxZoom={1.6}
        nodesConnectable={false}
        deleteKeyCode={null}
        onNodeDoubleClick={(_, node) => onSelect(node.data.instance)}
        onNodeDragStop={(_, node) => {
          const targetNode = nodes.find(
            (target) =>
              target.id !== node.id &&
              Math.abs(target.position.x - node.position.x) < 130 &&
              Math.abs(target.position.y - node.position.y) <
                (compact ? 65 : 100),
          );
          if (!targetNode) return;
          const positions = layoutInstances(visible, compact);
          setNodes((old) =>
            old.map((item) => ({
              ...item,
              position: positions.get(item.id) || item.position,
            })),
          );
          if (!config.authorizedForAction) {
            void message.info("当前只读或集群暂不可写，可查看和调整图形布局");
            return;
          }
          const decision = moveDecision(
            node.data.instance,
            targetNode.data.instance,
            instances,
            mode,
          );
          if (!decision.action) {
            void message.warning(decision.reason);
            return;
          }
          operate(
            decision.action,
            {
              instance: decision.useTargetAsSource
                ? targetNode.data.instance.Key
                : node.data.instance.Key,
            },
            {
              targetHost: targetNode.data.instance.Key.Hostname,
              targetPort: targetNode.data.instance.Key.Port,
            },
          );
        }}
      >
        <Background color="#dce3ed" gap={22} />
        <Controls showInteractive={false} />
        <MiniMap
          nodeColor={(node) =>
            instanceState((node as InstanceNode).data.instance).severity === 3
              ? "#ff7875"
              : "#91caff"
          }
          pannable
          zoomable
        />
      </ReactFlow>
      {colorize && (
        <Space className="dc-legend" wrap>
          {dc.map((name, index) => (
            <Tag key={name} color={colors[index % colors.length]}>
              {name || "未标注机房"}
            </Tag>
          ))}
        </Space>
      )}
    </div>
  );
}
