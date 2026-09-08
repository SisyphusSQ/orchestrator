import { useState } from "react";
import {
  Alert,
  Button,
  Card,
  Input,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import { Link } from "react-router-dom";
import { useQuery, refreshAll } from "../api/use-query";
import type { Analysis, BlockedRecovery, InstanceKey } from "../api/types";
import { useConfig } from "../app/context";
import { instanceID } from "../domain/instance";
import {
  clusterLink,
  InstanceLink,
  PageTitle,
  QueryError,
} from "../components/common";
import { InstanceDrawer } from "../components/instance-drawer";
import { useOperation } from "../components/operations";

export function AnalysisPage() {
  const analysis = useQuery<Analysis[]>("/replication-analysis");
  const blocked = useQuery<BlockedRecovery[]>("/blocked-recoveries");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<InstanceKey>();
  const config = useConfig();
  const operate = useOperation();
  const rows = (analysis.data || [])
    .filter(
      (item) => item.Analysis !== "NoProblem" || item.StructureAnalysis?.length,
    )
    .filter((item) =>
      `${item.Analysis} ${instanceID(item.AnalyzedInstanceKey)} ${item.ClusterDetails.ClusterAlias}`
        .toLowerCase()
        .includes(search.toLowerCase()),
    );
  return (
    <>
      <PageTitle
        title="故障分析"
        description="结合复制状态和集群结构识别风险，并跟踪恢复阻塞。"
      />
      <QueryError error={analysis.error || blocked.error} retry={refreshAll} />
      {!!blocked.data?.length && (
        <Alert
          type="warning"
          showIcon
          title={`${blocked.data.length} 项恢复受阻`}
          description={
            <Space orientation="vertical">
              {blocked.data.map((item) => (
                <span key={instanceID(item.FailedInstanceKey)}>
                  <InstanceLink value={item.FailedInstanceKey} /> ·{" "}
                  {item.Analysis} ·{" "}
                  <Link to={`/audit-recovery/id/${item.BlockingRecoveryId}`}>
                    查看阻塞恢复 #{item.BlockingRecoveryId}
                  </Link>
                </span>
              ))}
            </Space>
          }
        />
      )}
      <Card
        title={
          <Space>
            分析结果<Tag>{rows.length}</Tag>
          </Space>
        }
        extra={
          <Input.Search
            aria-label="搜索故障"
            placeholder="搜索实例或分析类型"
            onChange={(event) => setSearch(event.target.value)}
            allowClear
            style={{ width: 260 }}
          />
        }
      >
        <Table
          rowKey={(item) =>
            `${instanceID(item.AnalyzedInstanceKey)}:${item.Analysis}`
          }
          dataSource={rows}
          loading={analysis.loading && !analysis.data}
          scroll={{ x: 1000 }}
          columns={[
            {
              title: "分析",
              key: "analysis",
              render: (_, item) => (
                <Space orientation="vertical" size={4}>
                  <Tag color={item.IsDowntimed ? "default" : "error"}>
                    {item.Analysis}
                  </Tag>
                  {item.IsDowntimed && (
                    <Typography.Text type="secondary">
                      实例处于停机期
                    </Typography.Text>
                  )}
                  {item.StructureAnalysis?.map((problem) => (
                    <Tag key={problem} color="warning">
                      {problem}
                    </Tag>
                  ))}
                </Space>
              ),
            },
            {
              title: "实例",
              key: "instance",
              render: (_, item) => (
                <Button
                  className="table-link mono"
                  type="link"
                  onClick={() => setSelected(item.AnalyzedInstanceKey)}
                >
                  {instanceID(item.AnalyzedInstanceKey)}
                </Button>
              ),
            },
            {
              title: "集群",
              key: "cluster",
              render: (_, item) => (
                <Link to={clusterLink(item.ClusterDetails.ClusterName)}>
                  {item.ClusterDetails.ClusterAlias ||
                    item.ClusterDetails.ClusterName}
                </Link>
              ),
            },
            {
              title: "有效 / 复制中 / 总副本",
              key: "replicas",
              render: (_, item) =>
                `${item.CountValidReplicas} / ${item.CountValidReplicatingReplicas} / ${item.CountReplicas}`,
            },
            {
              title: "操作",
              key: "action",
              render: (_, item) => (
                <Button
                  disabled={
                    !config.authorizedForAction || item.Analysis === "NoProblem"
                  }
                  onClick={() =>
                    operate("recover", { instance: item.AnalyzedInstanceKey })
                  }
                >
                  执行恢复
                </Button>
              ),
            },
          ]}
          locale={{
            emptyText: analysis.error
              ? "读取失败，无法判断故障状态"
              : "暂无匹配的故障分析",
          }}
        />
      </Card>
      <InstanceDrawer
        selected={selected}
        onClose={() => setSelected(undefined)}
      />
    </>
  );
}
