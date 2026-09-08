import {
  Button,
  Card,
  Descriptions,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { useQuery } from "../api/use-query";
import { useConfig } from "../app/context";
import { PageTitle, QueryError } from "../components/common";
import { useOperation } from "../components/operations";

interface Health {
  Hostname: string;
  Healthy: boolean;
  AvailableNodes: {
    Hostname: string;
    FirstSeenActive: string;
    DBBackend: string;
    AppVersion: string;
  }[];
  RaftLeader: string;
  RaftLeaderURI: string;
  RaftNodeID: string;
  RaftMembers: string[];
  IsRaftLeader: boolean;
}
export function StatusPage() {
  const data = useQuery<Health>("/health");
  const config = useConfig();
  const operate = useOperation();
  const health = data.data;
  const leaderURL =
    health?.RaftLeaderURI && /^https?:\/\//.test(health.RaftLeaderURI)
      ? health.RaftLeaderURI
      : undefined;
  return (
    <>
      <PageTitle
        title="系统状态"
        description="查看 Orchestrator 服务节点、Raft 状态和当前访问权限。"
      />
      <QueryError error={data.error} retry={data.refresh} />
      <Card title="当前连接">
        <Descriptions
          column={2}
          items={[
            {
              key: "node",
              label: "服务节点",
              children: health?.Hostname || "—",
            },
            {
              key: "user",
              label: "用户",
              children: config.userId || "未提供用户名",
            },
            {
              key: "permission",
              label: "操作权限",
              children: (
                <Tag color={config.authorizedForAction ? "blue" : "default"}>
                  {config.authorizedForAction ? "允许操作" : "只读或集群不可写"}
                </Tag>
              ),
            },
            {
              key: "agents",
              label: "Agent 服务",
              children: config.agentsEnabled ? "已启用" : "未启用",
            },
          ]}
        />
      </Card>
      <Card className="section-gap" title="服务节点">
        <Table
          rowKey={(row) => `${row.Hostname}:${row.FirstSeenActive}`}
          dataSource={health?.AvailableNodes || []}
          columns={[
            { title: "主机", dataIndex: "Hostname", key: "host" },
            {
              title: "运行起始时间",
              dataIndex: "FirstSeenActive",
              key: "time",
            },
            { title: "元数据库", dataIndex: "DBBackend", key: "backend" },
            { title: "版本", dataIndex: "AppVersion", key: "version" },
          ]}
        />
      </Card>
      {health?.RaftLeader && (
        <Card className="section-gap" title="Raft 集群">
          <Descriptions
            column={1}
            items={[
              { key: "id", label: "当前节点", children: health.RaftNodeID },
              { key: "leader", label: "Leader", children: health.RaftLeader },
              {
                key: "uri",
                label: "Leader 地址",
                children: leaderURL ? (
                  <a href={leaderURL} rel="noopener noreferrer">
                    {leaderURL}
                  </a>
                ) : (
                  health.RaftLeaderURI
                ),
              },
              {
                key: "members",
                label: "成员",
                children: (
                  <Space wrap>
                    {health.RaftMembers?.map((member) => (
                      <Tag key={member}>{member}</Tag>
                    ))}
                  </Space>
                ),
              },
            ]}
          />
        </Card>
      )}
      <Card className="section-gap" title="服务操作">
        <Space wrap>
          {[
            ["reload-configuration", "重新加载配置"],
            ["reset-hostname-resolve-cache", "清空主机解析缓存"],
            ["reelect", "重新选举活动节点"],
          ].map(([id, label]) => (
            <Button
              key={id}
              disabled={!config.authorizedForAction}
              onClick={() => operate(id)}
            >
              {label}
            </Button>
          ))}
        </Space>
      </Card>
    </>
  );
}
export function HelpPage() {
  return (
    <>
      <PageTitle
        title="关于 Orchestrator"
        description="MySQL 高可用与复制拓扑管理。"
      />
      <Card title="从发现实例开始">
        <Typography.Paragraph>
          添加一个 MySQL 地址，Orchestrator
          会沿复制关系发现上下游。在集群总览进入拓扑，点击实例名称查看详情，双击节点也可打开详情。
        </Typography.Paragraph>
        <Typography.Paragraph>
          拓扑支持缩放、折叠、机房标识与实例列表。拖动节点可调整布局；拖到另一实例上会提出复制调整，并在确认后交由服务端执行。
        </Typography.Paragraph>
      </Card>
      <Card className="section-gap" title="操作与恢复">
        <Typography.Paragraph>
          只读用户可查看状态；具备写权限且集群可写时可进行维护、复制调整和恢复。请求失败或结果未知时，先回读实际状态，避免重复操作。
        </Typography.Paragraph>
        <Typography.Paragraph>
          检测、恢复和审计记录提供相互关联的入口。恢复详情包含接任实例、参与实例、丢失副本、错误与执行步骤。
        </Typography.Paragraph>
        <Space>
          <a
            href="https://github.com/SisyphusSQ/orchestrator"
            target="_blank"
            rel="noreferrer"
          >
            项目源码与文档
          </a>
          <a
            href="https://github.com/percona/orchestrator"
            target="_blank"
            rel="noreferrer"
          >
            上游项目
          </a>
        </Space>
      </Card>
    </>
  );
}
