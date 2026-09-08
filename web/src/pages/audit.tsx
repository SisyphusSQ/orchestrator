import { useState } from "react";
import {
  Alert,
  Button,
  Card,
  Collapse,
  Descriptions,
  Drawer,
  Flex,
  Space,
  Switch,
  Table,
  Tag,
  Timeline,
  Typography,
} from "antd";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQuery, refreshAll } from "../api/use-query";
import { endpoint } from "../api/client";
import type { Audit, Recovery, RecoveryStep } from "../api/types";
import { useConfig } from "../app/context";
import { instanceID } from "../domain/instance";
import {
  clusterLink,
  InstanceLink,
  JsonDetails,
  PageTitle,
  QueryError,
} from "../components/common";
import { useOperation } from "../components/operations";

function RecoveryDetails({
  recovery,
  onClose,
  detection,
}: {
  recovery?: Recovery;
  onClose: () => void;
  detection: boolean;
}) {
  const steps = useQuery<RecoveryStep[]>(
    recovery?.UID && !detection
      ? endpoint("audit-recovery-steps", recovery.UID)
      : null,
  );
  const config = useConfig();
  const changelog = useQuery<
    {
      AnalyzedInstanceKey: import("../api/types").InstanceKey;
      Changelog: string[];
    }[]
  >(detection && recovery ? "/replication-analysis-changelog" : null);
  const operate = useOperation();
  return (
    <Drawer
      open={!!recovery}
      onClose={onClose}
      title={`${detection ? "故障检测" : "恢复详情"} #${recovery?.Id || ""}`}
      size={680}
    >
      {recovery && (
        <>
          <Descriptions
            column={1}
            size="small"
            bordered
            items={[
              {
                key: "analysis",
                label: "分析",
                children: recovery.AnalysisEntry.Analysis,
              },
              {
                key: "instance",
                label: "故障实例",
                children: (
                  <InstanceLink
                    value={recovery.AnalysisEntry.AnalyzedInstanceKey}
                  />
                ),
              },
              {
                key: "successor",
                label: "接任实例",
                children: <InstanceLink value={recovery.SuccessorKey} />,
              },
              {
                key: "start",
                label: "开始时间",
                children: recovery.RecoveryStartTimestamp,
              },
              {
                key: "end",
                label: "结束时间",
                children: recovery.RecoveryEndTimestamp || "未结束",
              },
              {
                key: "node",
                label: "执行节点",
                children: recovery.ProcessingNodeHostname,
              },
              {
                key: "detection",
                label: "关联检测",
                children: recovery.LastDetectionId ? (
                  <Link
                    to={`/audit-failure-detection/id/${recovery.LastDetectionId}`}
                    onClick={onClose}
                  >
                    #{recovery.LastDetectionId}
                  </Link>
                ) : (
                  "—"
                ),
              },
              {
                key: "ack",
                label: "确认",
                children: recovery.Acknowledged
                  ? `${recovery.AcknowledgedBy} · ${recovery.AcknowledgedAt} · ${recovery.AcknowledgedComment}`
                  : "未确认",
              },
            ]}
          />
          {!detection && !recovery.Acknowledged && (
            <Button
              className="section-gap"
              disabled={!config.authorizedForAction}
              onClick={() => operate("ack-recovery", { recovery: recovery.Id })}
            >
              确认此恢复记录
            </Button>
          )}
          {!detection && (
            <>
              <Typography.Title level={5}>恢复过程</Typography.Title>
              <QueryError error={steps.error} retry={steps.refresh} />
              <Timeline
                items={(steps.data || []).map((step) => ({
                  title: step.AuditAt,
                  content: step.Message,
                }))}
              />
            </>
          )}
          {detection && (
            <>
              <Typography.Title level={5}>故障发生时的副本</Typography.Title>
              <Space wrap>
                {recovery.AnalysisEntry.Replicas?.map((key) => (
                  <InstanceLink key={instanceID(key)} value={key} />
                ))}
              </Space>
              {recovery.RelatedRecoveryId ? (
                <Typography.Paragraph>
                  <Link to={`/audit-recovery/id/${recovery.RelatedRecoveryId}`}>
                    关联恢复 #{recovery.RelatedRecoveryId}
                  </Link>
                </Typography.Paragraph>
              ) : null}
              <Typography.Title level={5}>检测历史</Typography.Title>
              <QueryError error={changelog.error} retry={changelog.refresh} />
              <Timeline
                items={(
                  changelog.data?.find(
                    (entry) =>
                      instanceID(entry.AnalyzedInstanceKey) ===
                      instanceID(recovery.AnalysisEntry.AnalyzedInstanceKey),
                  )?.Changelog || []
                )
                  .filter(
                    (entry) =>
                      entry.split(";")[0] <= recovery.RecoveryStartTimestamp,
                  )
                  .slice()
                  .reverse()
                  .map((entry) => ({
                    title: entry.split(";")[0],
                    content: entry.split(";").slice(1).join(";"),
                  }))}
              />
            </>
          )}
          <Collapse
            items={[
              {
                key: "participants",
                label: `参与实例 ${recovery.ParticipatingInstanceKeys?.length || 0} · 丢失副本 ${recovery.LostReplicas?.length || 0}`,
                children: (
                  <>
                    <Typography.Text>参与实例</Typography.Text>
                    <Space wrap>
                      {recovery.ParticipatingInstanceKeys?.map((key) => (
                        <InstanceLink key={instanceID(key)} value={key} />
                      ))}
                    </Space>
                    <Typography.Paragraph>丢失副本</Typography.Paragraph>
                    <Space wrap>
                      {recovery.LostReplicas?.map((key) => (
                        <InstanceLink key={instanceID(key)} value={key} />
                      ))}
                    </Space>
                  </>
                ),
              },
              {
                key: "errors",
                label: `错误信息 ${recovery.AllErrors?.length || 0}`,
                children: <JsonDetails value={recovery.AllErrors || []} />,
              },
            ]}
          />
        </>
      )}
    </Drawer>
  );
}

export function AuditPage({
  kind,
}: {
  kind: "audit" | "audit-recovery" | "audit-failure-detection";
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const config = useConfig();
  const operate = useOperation();
  const [selected, setSelected] = useState<Recovery>();
  const [unack, setUnack] = useState(false);
  const parts = location.pathname
    .split("/")
    .filter(Boolean)
    .map((part) => decodeURIComponent(part));
  const explicitDetail =
    parts.includes("id") ||
    parts.includes("uid") ||
    parts[0] === "audit-recovery-steps";
  const last = parts.at(-1) || "";
  const hasPage =
    !explicitDetail &&
    /^[0-9]+$/.test(last) &&
    parts[parts.length - 2] !== "instance" &&
    !(parts[1] === "instance" && parts.length === 4);
  const page = hasPage ? Number(last) : 0;
  const base = hasPage ? parts.slice(0, -1) : parts;
  const path =
    parts[0] === "audit-recovery-steps"
      ? endpoint("audit-recovery", "uid", parts[1])
      : endpoint(...(explicitDetail ? parts : [...base, page]));
  const data = useQuery<Audit[] | Recovery[]>(
    path + (unack ? "?unacknowledged=true" : ""),
  );
  const rows = data.data || [];
  const detection = kind === "audit-failure-detection";
  const title =
    kind === "audit" ? "操作审计" : detection ? "故障检测" : "恢复记录";
  const detail =
    (selected &&
      ((rows as Recovery[]).find((row) => row.Id === selected.Id) ||
        selected)) ||
    (explicitDetail && kind !== "audit" ? (rows as Recovery[])[0] : undefined);
  const turnPage = (next: number) => navigate(endpoint(...base, next));
  return (
    <>
      <PageTitle
        title={title}
        description={
          kind === "audit"
            ? "追踪实例管理、拓扑调整与维护操作。"
            : detection
              ? "查看检测到的故障及其关联的恢复过程。"
              : "跟踪故障恢复的执行结果、参与实例和确认状态。"
        }
        extra={
          kind === "audit-recovery" && !explicitDetail ? (
            <Space>
              仅看未确认{" "}
              <Switch
                checked={unack}
                onChange={(value) => {
                  setUnack(value);
                  if (page) turnPage(0);
                }}
              />
            </Space>
          ) : undefined
        }
      />
      <QueryError error={data.error} retry={refreshAll} />
      {kind === "audit" && config.auditEnabled === false && (
        <Alert
          className="query-error"
          type="info"
          showIcon
          title="服务端未启用审计写库"
          description="此页面只能查询元数据库中的历史审计。需要记录新操作时，请在服务端启用 AuditToBackendDB。"
        />
      )}
      <Card className="table-card">
        {kind === "audit" ? (
          <Table<Audit>
            rowKey="AuditId"
            dataSource={rows as Audit[]}
            loading={data.loading && !data.data}
            pagination={false}
            scroll={{ x: 1000 }}
            columns={[
              {
                title: "时间",
                dataIndex: "AuditTimestamp",
                key: "time",
                width: 185,
              },
              {
                title: "操作类型",
                dataIndex: "AuditType",
                key: "type",
                render: (value) => <Tag>{value}</Tag>,
              },
              {
                title: "实例",
                key: "instance",
                render: (_, row) => (
                  <InstanceLink value={row.AuditInstanceKey} />
                ),
              },
              {
                title: "详情",
                dataIndex: "Message",
                key: "message",
                render: (value) => <span className="wrap-text">{value}</span>,
              },
            ]}
          />
        ) : (
          <Table<Recovery>
            rowKey="Id"
            dataSource={rows as Recovery[]}
            loading={data.loading && !data.data}
            pagination={false}
            scroll={{ x: 1150 }}
            columns={[
              {
                title: "记录",
                key: "id",
                render: (_, row) => (
                  <Button type="link" onClick={() => setSelected(row)}>
                    #{row.Id}
                  </Button>
                ),
              },
              {
                title: "开始时间",
                dataIndex: "RecoveryStartTimestamp",
                key: "time",
                width: 185,
              },
              {
                title: "故障分析",
                key: "analysis",
                render: (_, row) => (
                  <Space orientation="vertical" size={4}>
                    <Tag color="warning">{row.AnalysisEntry.Analysis}</Tag>
                    <InstanceLink
                      value={row.AnalysisEntry.AnalyzedInstanceKey}
                    />
                  </Space>
                ),
              },
              {
                title: "集群",
                key: "cluster",
                render: (_, row) => (
                  <Link
                    to={clusterLink(
                      row.AnalysisEntry.ClusterDetails.ClusterName,
                    )}
                  >
                    {row.AnalysisEntry.ClusterDetails.ClusterAlias ||
                      row.AnalysisEntry.ClusterDetails.ClusterName}
                  </Link>
                ),
              },
              {
                title: "状态",
                key: "status",
                render: (_, row) =>
                  detection ? (
                    <Tag color="blue">已检测</Tag>
                  ) : (
                    <Tag
                      color={
                        row.IsActive
                          ? "processing"
                          : row.IsSuccessful
                            ? "success"
                            : "error"
                      }
                    >
                      {row.IsActive
                        ? "恢复中"
                        : row.IsSuccessful
                          ? "恢复成功"
                          : "恢复失败"}
                    </Tag>
                  ),
              },
              ...(!detection
                ? [
                    {
                      title: "确认",
                      key: "ack",
                      render: (_: unknown, row: Recovery) =>
                        row.Acknowledged ? (
                          <Tag>已确认 · {row.AcknowledgedBy}</Tag>
                        ) : (
                          <Button
                            size="small"
                            disabled={!config.authorizedForAction}
                            onClick={() =>
                              operate("ack-recovery", { recovery: row.Id })
                            }
                          >
                            确认记录
                          </Button>
                        ),
                    },
                  ]
                : []),
            ]}
          />
        )}
        {!explicitDetail && (
          <Flex
            className="table-pagination"
            justify="space-between"
            align="center"
          >
            <Typography.Text type="secondary">
              第 {page + 1} 页 · 本页 {rows.length} 条
            </Typography.Text>
            <Space>
              <Button
                disabled={page === 0 || !!data.error}
                onClick={() => turnPage(page - 1)}
              >
                上一页
              </Button>
              <Button
                disabled={rows.length < config.auditPageSize || !!data.error}
                onClick={() => turnPage(page + 1)}
              >
                下一页
              </Button>
            </Space>
          </Flex>
        )}
      </Card>
      <RecoveryDetails
        recovery={detail}
        detection={detection}
        onClose={() => {
          setSelected(undefined);
          if (explicitDetail) navigate(`/${kind}`);
        }}
      />
    </>
  );
}
