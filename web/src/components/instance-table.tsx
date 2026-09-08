import { Button, Space, Table, Tag } from "antd";
import type { TableProps } from "antd";
import type { Instance } from "../api/types";
import {
  instanceID,
  instanceState,
  lagValue,
  lagText,
  role,
  replicationMode,
} from "../domain/instance";
import { InstanceTag } from "./common";

export function InstanceTable({
  instances,
  onSelect,
  loading,
}: {
  instances: Instance[];
  onSelect: (instance: Instance) => void;
  loading?: boolean;
}) {
  const columns: TableProps<Instance>["columns"] = [
    {
      title: "实例",
      key: "instance",
      render: (_, instance) => (
        <Button
          type="link"
          className="table-link mono"
          onClick={() => onSelect(instance)}
        >
          {instanceID(instance.Key)}
        </Button>
      ),
      sorter: (a, b) => instanceID(a.Key).localeCompare(instanceID(b.Key)),
    },
    {
      title: "角色",
      key: "role",
      render: (_, instance) => (
        <Tag color={role(instance) === "副本" ? "default" : "blue"}>
          {role(instance)}
        </Tag>
      ),
    },
    {
      title: "状态",
      key: "status",
      render: (_, instance) => <InstanceTag instance={instance} />,
      sorter: (a, b) => instanceState(a).severity - instanceState(b).severity,
    },
    {
      title: "复制延迟",
      key: "lag",
      render: (_, instance) => lagText(instance),
      sorter: (a, b) =>
        (lagValue(a.ReplicationLagSeconds) ?? -1) -
        (lagValue(b.ReplicationLagSeconds) ?? -1),
    },
    {
      title: "机房 / 环境",
      key: "dc",
      render: (_, instance) => (
        <Space orientation="vertical" size={0}>
          <span>{instance.DataCenter || "—"}</span>
          <span className="muted">{instance.PhysicalEnvironment || "—"}</span>
        </Space>
      ),
    },
    {
      title: "复制模式",
      key: "mode",
      render: (_, instance) => replicationMode(instance),
    },
    { title: "版本", dataIndex: "Version", key: "version" },
  ];
  return (
    <Table<Instance>
      rowKey={(instance) => instanceID(instance.Key)}
      columns={columns}
      dataSource={instances}
      loading={loading}
      scroll={{ x: 900 }}
      pagination={{
        defaultPageSize: 20,
        showSizeChanger: true,
        showTotal: (total) => `共 ${total} 个实例`,
      }}
    />
  );
}
