import { useMemo, useState } from "react";
import {
  Button,
  Card,
  Flex,
  Input,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from "antd";
import type { TableProps } from "antd";
import {
  ArrowRightOutlined,
  DatabaseOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import { Link } from "react-router-dom";
import { useQuery, refreshAll } from "../api/use-query";
import type { Analysis, Cluster, Instance } from "../api/types";
import { useConfig } from "../app/context";
import { clusterLink, PageTitle, QueryError } from "../components/common";
import { instanceState } from "../domain/instance";

export function ClustersPage() {
  const clusters = useQuery<Cluster[]>("/clusters-info");
  const analysis = useQuery<Analysis[]>("/replication-analysis");
  const problems = useQuery<Instance[]>("/problems");
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState("all");
  const config = useConfig();
  const rows = useMemo(
    () =>
      (clusters.data || [])
        .map((cluster) => ({
          ...cluster,
          problems: (problems.data || []).filter(
            (instance) => instance.ClusterName === cluster.ClusterName,
          ),
          analysis: (analysis.data || []).filter(
            (item) =>
              item.ClusterDetails.ClusterName === cluster.ClusterName &&
              item.Analysis !== "NoProblem",
          ),
        }))
        .sort(
          (a, b) =>
            b.problems.length +
              b.analysis.length -
              (a.problems.length + a.analysis.length) ||
            a.ClusterName.localeCompare(b.ClusterName),
        ),
    [clusters.data, analysis.data, problems.data],
  );
  const hasError = clusters.error || analysis.error || problems.error;
  const healthUnknown =
    !!hasError || analysis.data === undefined || problems.data === undefined;
  const abnormal = rows.filter(
    (row) => row.problems.length || row.analysis.length,
  ).length;
  const filtered = rows.filter(
    (row) =>
      (filter === "all" ||
        (filter === "abnormal"
          ? row.problems.length + row.analysis.length > 0
          : row.problems.length + row.analysis.length === 0)) &&
      `${row.ClusterName} ${row.ClusterAlias} ${row.ClusterDomain}`
        .toLowerCase()
        .includes(search.toLowerCase()),
  );
  const columns: TableProps<(typeof rows)[number]>["columns"] = [
    {
      title: "集群",
      key: "name",
      width: 300,
      render: (_, row) => (
        <Space orientation="vertical" size={3}>
          <Link className="cluster-name" to={clusterLink(row.ClusterName)}>
            {row.ClusterAlias || row.ClusterName}
          </Link>
          <Typography.Text type="secondary" className="mono">
            {row.ClusterName}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: "健康状态",
      key: "health",
      render: (_, row) =>
        healthUnknown ? (
          <Tag>待确认</Tag>
        ) : row.problems.length || row.analysis.length ? (
          <Tag color="error">
            需要关注 · {Math.max(row.problems.length, row.analysis.length)}
          </Tag>
        ) : (
          <Tag color="success">运行正常</Tag>
        ),
    },
    {
      title: "实例",
      dataIndex: "CountInstances",
      key: "count",
      sorter: (a, b) => a.CountInstances - b.CountInstances,
    },
    {
      title: "自动恢复",
      key: "recovery",
      render: (_, row) => (
        <Space orientation="vertical" size={2}>
          <span>
            <span
              className={`state-dot ${row.HasAutomatedMasterRecovery ? "green" : "gray"}`}
            />
            主库 {row.HasAutomatedMasterRecovery ? "已启用" : "未启用"}
          </span>
          <Typography.Text type="secondary">
            中间主库{" "}
            {row.HasAutomatedIntermediateMasterRecovery ? "已启用" : "未启用"}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: "当前问题",
      key: "problem",
      render: (_, row) => (
        <span className="muted">
          {row.analysis[0]?.Analysis ||
            (row.problems[0] && instanceState(row.problems[0]).label) ||
            "—"}
        </span>
      ),
    },
    {
      title: "",
      key: "enter",
      width: 110,
      render: (_, row) => (
        <Link to={clusterLink(row.ClusterName)}>
          查看拓扑 <ArrowRightOutlined />
        </Link>
      ),
    },
  ];
  return (
    <>
      <PageTitle
        title="集群总览"
        description="集中查看 MySQL 复制拓扑、运行状态与恢复策略。"
        extra={
          <Link to="/discover">
            <Button
              aria-label="发现实例"
              type="primary"
              icon={<PlusOutlined />}
              disabled={!config.authorizedForAction}
            >
              发现实例
            </Button>
          </Link>
        }
      />
      <QueryError error={hasError} retry={refreshAll} />
      <div className="stats-grid">
        <Card>
          <Statistic
            title="集群总数"
            value={clusters.data?.length ?? "—"}
            prefix={<DatabaseOutlined />}
          />
          <span className="stat-note">已发现的复制集群</span>
        </Card>
        <Card>
          <Statistic
            title="实例总数"
            value={
              clusters.data?.reduce(
                (sum, row) => sum + row.CountInstances,
                0,
              ) ?? "—"
            }
          />
          <span className="stat-note">主库与副本</span>
        </Card>
        <Card>
          <Statistic
            title="需要关注"
            value={healthUnknown ? "—" : abnormal}
            prefix={<WarningOutlined />}
            styles={{ content: { color: abnormal ? "#d46b08" : undefined } }}
          />
          <span className="stat-note">存在异常的集群</span>
        </Card>
        <Card>
          <Statistic
            title="自动主库恢复"
            value={
              clusters.data?.filter((row) => row.HasAutomatedMasterRecovery)
                .length ?? "—"
            }
            prefix={<SafetyCertificateOutlined />}
          />
          <span className="stat-note">已配置自动恢复策略</span>
        </Card>
      </div>
      <Card
        className="table-card"
        title={
          <Space>
            复制集群<Tag>{rows.length}</Tag>
          </Space>
        }
      >
        <Flex className="table-toolbar" justify="space-between" gap={12} wrap>
          <Input.Search
            aria-label="搜索集群"
            placeholder="搜索集群名称、别名或域名"
            allowClear
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: 330 }}
          />
          <Select
            aria-label="筛选集群状态"
            value={filter}
            onChange={setFilter}
            style={{ width: 160 }}
            options={[
              { value: "all", label: "全部状态" },
              { value: "abnormal", label: "需要关注" },
              { value: "healthy", label: "运行正常" },
            ]}
          />
        </Flex>
        <Table
          rowKey="ClusterName"
          columns={columns}
          dataSource={filtered}
          loading={clusters.loading && !clusters.data}
          scroll={{ x: 1050 }}
          pagination={{
            defaultPageSize: 10,
            showTotal: (total) => `共 ${total} 个集群`,
          }}
          locale={{
            emptyText: (
              <div className="empty-block">
                {search ? "没有匹配的集群" : "尚未发现集群"}
                <p className="muted">
                  {search
                    ? "调整搜索条件后重试。"
                    : "添加一个 MySQL 实例，Orchestrator 将发现它的复制拓扑。"}
                </p>
                {!search && (
                  <Link to="/discover">
                    <Button disabled={!config.authorizedForAction}>
                      发现第一个实例
                    </Button>
                  </Link>
                )}
              </div>
            ),
          }}
        />
      </Card>
    </>
  );
}
