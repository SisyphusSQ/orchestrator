import {
  Button,
  Card,
  Descriptions,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
} from "antd";
import { Link, useLocation } from "react-router-dom";
import { endpoint } from "../api/client";
import { useQuery, refreshAll } from "../api/use-query";
import type { Agent, Seed, SeedState } from "../api/types";
import { useConfig } from "../app/context";
import { PageTitle, QueryError } from "../components/common";
import { useOperation } from "../components/operations";

function SeedTable({ rows, loading }: { rows: Seed[]; loading?: boolean }) {
  const config = useConfig();
  const operate = useOperation();
  return (
    <Table<Seed>
      rowKey="SeedId"
      dataSource={rows}
      loading={loading}
      scroll={{ x: 900 }}
      columns={[
        {
          title: "任务",
          key: "id",
          render: (_, row) => (
            <Link to={`/seed-details/${row.SeedId}`}>#{row.SeedId}</Link>
          ),
        },
        {
          title: "状态",
          key: "status",
          render: (_, row) => (
            <Tag
              color={
                !row.IsComplete
                  ? "processing"
                  : row.IsSuccessful
                    ? "success"
                    : "error"
              }
            >
              {!row.IsComplete ? "进行中" : row.IsSuccessful ? "成功" : "失败"}
            </Tag>
          ),
        },
        {
          title: "目标主机",
          key: "target",
          render: (_, row) => (
            <Link to={`/agent/${encodeURIComponent(row.TargetHostname)}`}>
              {row.TargetHostname}
            </Link>
          ),
        },
        {
          title: "源主机",
          key: "source",
          render: (_, row) => (
            <Link to={`/agent/${encodeURIComponent(row.SourceHostname)}`}>
              {row.SourceHostname}
            </Link>
          ),
        },
        { title: "开始时间", dataIndex: "StartTimestamp", key: "start" },
        { title: "结束时间", dataIndex: "EndTimestamp", key: "end" },
        {
          title: "操作",
          key: "action",
          render: (_, row) =>
            !row.IsComplete && (
              <Button
                danger
                size="small"
                disabled={!config.authorizedForAction}
                onClick={() =>
                  operate("agent-abort-seed", { seed: row.SeedId })
                }
              >
                中止
              </Button>
            ),
        },
      ]}
    />
  );
}
export function SeedsPage() {
  const location = useLocation();
  const id = location.pathname.startsWith("/seed-details/")
    ? location.pathname.split("/")[2]
    : undefined;
  const seeds = useQuery<Seed[]>(
    id ? endpoint("agent-seed-details", id) : "/seeds",
  );
  const states = useQuery<SeedState[]>(
    id ? endpoint("agent-seed-states", id) : null,
  );
  return (
    <>
      <PageTitle
        title={id ? `数据恢复任务 #${id}` : "数据恢复任务"}
        description="查看 Agent 快照恢复任务及每个执行步骤。"
      />
      <QueryError error={seeds.error || states.error} retry={refreshAll} />
      <Card>
        <SeedTable
          rows={seeds.data || []}
          loading={seeds.loading && !seeds.data}
        />
      </Card>
      {id && (
        <Card className="section-gap" title="执行步骤">
          <Table<SeedState>
            rowKey="SeedStateId"
            dataSource={states.data || []}
            columns={[
              { title: "时间", dataIndex: "StateTimestamp", key: "time" },
              { title: "步骤", dataIndex: "Action", key: "action" },
              {
                title: "错误",
                dataIndex: "ErrorMessage",
                key: "error",
                render: (value) => (
                  <Typography.Text type={value ? "danger" : undefined}>
                    {value || "—"}
                  </Typography.Text>
                ),
              },
            ]}
          />
        </Card>
      )}
    </>
  );
}
export function AgentsPage() {
  const data = useQuery<Agent[]>("/agents");
  return (
    <>
      <PageTitle
        title="Agents"
        description="查看主机代理、MySQL 服务和快照能力。"
      />
      <QueryError error={data.error} retry={data.refresh} />
      <Card>
        <Table<Agent>
          rowKey="Hostname"
          dataSource={data.data || []}
          loading={data.loading && !data.data}
          columns={[
            {
              title: "主机",
              key: "hostname",
              render: (_, row) => (
                <Link to={`/agent/${encodeURIComponent(row.Hostname)}`}>
                  {row.Hostname}
                </Link>
              ),
            },
            { title: "代理端口", dataIndex: "Port", key: "port" },
            { title: "MySQL 端口", dataIndex: "MySQLPort", key: "mysqlport" },
            {
              title: "MySQL 状态",
              key: "mysql",
              render: (_, row) => (
                <Tag color={row.MySQLRunning ? "success" : "default"}>
                  {row.MySQLRunning ? "运行中" : "已停止"}
                </Tag>
              ),
            },
            { title: "最近上报", dataIndex: "LastSubmitted", key: "time" },
          ]}
        />
      </Card>
    </>
  );
}
export function AgentPage() {
  const location = useLocation();
  const host = decodeURIComponent(location.pathname.split("/")[2] || "");
  const data = useQuery<Agent>(endpoint("agent", host));
  const seeds = useQuery<Seed[]>(endpoint("agent-recent-seeds", host));
  const active = useQuery<Seed[]>(endpoint("agent-active-seeds", host));
  const config = useConfig();
  const operate = useOperation();
  const agent = data.data;
  return (
    <>
      <PageTitle
        title={host}
        description="Agent 主机详情"
        extra={
          <Button
            disabled={!config.authorizedForAction}
            onClick={() =>
              operate(
                "discover",
                {},
                { targetHost: host, targetPort: agent?.MySQLPort || 3306 },
              )
            }
          >
            发现 MySQL 实例
          </Button>
        }
      />
      <QueryError
        error={data.error || seeds.error || active.error}
        retry={refreshAll}
      />
      {agent && (
        <Card>
          <Tabs
            items={[
              {
                key: "overview",
                label: "主机与服务",
                children: (
                  <>
                    <Descriptions
                      column={2}
                      bordered
                      items={[
                        {
                          key: "host",
                          label: "主机",
                          children: agent.Hostname,
                        },
                        {
                          key: "port",
                          label: "代理端口",
                          children: agent.Port,
                        },
                        {
                          key: "mysql",
                          label: "MySQL 状态",
                          children: agent.MySQLRunning ? "运行中" : "已停止",
                        },
                        {
                          key: "mysqlport",
                          label: "MySQL 端口",
                          children: agent.MySQLPort,
                        },
                        {
                          key: "disk",
                          label: "MySQL 磁盘用量",
                          children: `${((agent.MySQLDiskUsage || 0) / 1024 / 1024 / 1024).toFixed(2)} GB`,
                        },
                        {
                          key: "submitted",
                          label: "最近上报",
                          children: agent.LastSubmitted,
                        },
                      ]}
                    />
                    <Space className="section-gap" wrap>
                      {[
                        ["agent-mysql-start", "启动 MySQL"],
                        ["agent-mysql-stop", "停止 MySQL"],
                        ["agent-create-snapshot", "创建快照"],
                        ["agent-umount", "卸载数据卷"],
                      ].map(([id, label]) => (
                        <Button
                          key={id}
                          disabled={!config.authorizedForAction}
                          onClick={() => operate(id, { agent: host })}
                        >
                          {label}
                        </Button>
                      ))}
                    </Space>
                    <Typography.Title level={5}>
                      MySQL 错误日志
                    </Typography.Title>
                    <pre className="json-details">
                      {agent.MySQLErrorLogTail?.join("\n") || "暂无日志"}
                    </pre>
                  </>
                ),
              },
              {
                key: "volumes",
                label: "数据卷与快照",
                children: (
                  <>
                    <Descriptions
                      column={1}
                      bordered
                      items={[
                        {
                          key: "mount",
                          label: "挂载点",
                          children: agent.MountPoint?.Path || "—",
                        },
                        {
                          key: "device",
                          label: "设备",
                          children: agent.MountPoint?.Device || "—",
                        },
                        {
                          key: "mounted",
                          label: "已挂载",
                          children: agent.MountPoint?.IsMounted ? "是" : "否",
                        },
                      ]}
                    />
                    <Typography.Title level={5}>逻辑卷</Typography.Title>
                    <Table
                      rowKey="Path"
                      dataSource={agent.LogicalVolumes || []}
                      columns={[
                        { title: "逻辑卷", dataIndex: "Name", key: "name" },
                        { title: "卷组", dataIndex: "GroupName", key: "group" },
                        { title: "路径", dataIndex: "Path", key: "path" },
                        {
                          title: "操作",
                          key: "actions",
                          render: (_, row) => (
                            <Space>
                              <Button
                                disabled={!config.authorizedForAction}
                                onClick={() =>
                                  operate(
                                    "agent-mount",
                                    { agent: host },
                                    { lv: row.Path },
                                  )
                                }
                              >
                                挂载
                              </Button>
                              <Button
                                danger
                                disabled={!config.authorizedForAction}
                                onClick={() =>
                                  operate(
                                    "agent-removelv",
                                    { agent: host },
                                    { lv: row.Path },
                                  )
                                }
                              >
                                删除
                              </Button>
                            </Space>
                          ),
                        },
                      ]}
                    />
                    <Typography.Title level={5}>可用快照</Typography.Title>
                    <Space orientation="vertical">
                      {[
                        ...new Set([
                          ...(agent.AvailableLocalSnapshots || []),
                          ...(agent.AvailableSnapshots || []),
                        ]),
                      ].map((source) => (
                        <Space key={source}>
                          <span>{source}</span>
                          <Tag>
                            {agent.AvailableLocalSnapshots?.includes(source)
                              ? "本地"
                              : "远端"}
                          </Tag>
                          <Button
                            size="small"
                            disabled={
                              !config.authorizedForAction || agent.MySQLRunning
                            }
                            onClick={() =>
                              operate("agent-seed", { agent: host }, { source })
                            }
                          >
                            使用此快照恢复
                          </Button>
                        </Space>
                      ))}
                    </Space>
                  </>
                ),
              },
              {
                key: "active",
                label: `活动任务 (${active.data?.length || 0})`,
                children: <SeedTable rows={active.data || []} />,
              },
              {
                key: "seeds",
                label: "最近任务",
                children: <SeedTable rows={seeds.data || []} />,
              },
            ]}
          />
        </Card>
      )}
    </>
  );
}
