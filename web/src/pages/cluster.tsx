import { useCallback, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Dropdown,
  Flex,
  Input,
  Segmented,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";
import {
  MoreOutlined,
  UnorderedListOutlined,
  ApartmentOutlined,
} from "@ant-design/icons";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { endpoint } from "../api/client";
import { useQuery, refreshAll } from "../api/use-query";
import type {
  Analysis,
  BlockedRecovery,
  Cluster,
  Instance,
  InstanceKey,
  Maintenance,
  Recovery,
} from "../api/types";
import { useConfig } from "../app/context";
import { instanceID, instanceState } from "../domain/instance";
import type { MoveMode } from "../domain/topology";
import { InstanceDrawer } from "../components/instance-drawer";
import { InstanceTable } from "../components/instance-table";
import { Topology } from "../components/topology";
import {
  LoadingState,
  PageTitle,
  QueryError,
  NoData,
} from "../components/common";
import { useOperation } from "../components/operations";

export function ClusterPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const parts = location.pathname
    .split("/")
    .filter(Boolean)
    .map((part) => decodeURIComponent(part));
  const resource = endpoint(...parts);
  const data = useQuery<Instance[]>(resource);
  const maintenance = useQuery<Maintenance[]>("/maintenance");
  const cluster =
    parts[1] === "alias" || parts[1] === "instance"
      ? data.data?.[0]?.ClusterName
      : parts[1];
  const info = useQuery<Cluster>(
    cluster ? endpoint("cluster-info", cluster) : null,
  );
  const analyses = useQuery<Analysis[]>(
    cluster ? endpoint("replication-analysis", cluster) : null,
  );
  const recovery = useQuery<Recovery[]>(
    cluster ? endpoint("active-cluster-recovery", cluster) : null,
  );
  const recent = useQuery<Recovery[]>(
    cluster ? endpoint("recently-active-cluster-recovery", cluster) : null,
  );
  const blocked = useQuery<BlockedRecovery[]>(
    cluster ? endpoint("blocked-recoveries", "cluster", cluster) : null,
  );
  const [poolVisible, setPoolVisible] = useState(false);
  const pools = useQuery<Record<string, InstanceKey[]>>(
    cluster && poolVisible ? endpoint("cluster-pool-instances", cluster) : null,
  );
  const [selected, setSelected] = useState<Instance>();
  const select = useCallback((instance: Instance) => setSelected(instance), []);
  const [view, setView] = useState("topology");
  const [compact, setCompact] = useState(
    new URLSearchParams(location.search).get("compact") === "true",
  );
  const [colorize, setColorize] = useState(true);
  const [anonymize, setAnonymize] = useState(false);
  const [aliases, setAliases] = useState(false);
  const [mode, setMode] = useState<MoveMode>("smart");
  const [search, setSearch] = useState("");
  const config = useConfig();
  const operate = useOperation();
  const instances = data.data || [];
  const poolMap = useMemo(() => {
    const map: Record<string, string[]> = {};
    for (const [name, keys] of Object.entries(pools.data || {}))
      for (const key of keys) (map[instanceID(key)] ??= []).push(name);
    return map;
  }, [pools.data]);
  const filtered = instances.filter((instance) =>
    `${instanceID(instance.Key)} ${instance.InstanceAlias || ""}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const bad = instances.filter(
    (instance) => instanceState(instance).severity >= 2,
  ).length;
  return (
    <>
      <PageTitle
        title={
          anonymize
            ? "复制集群"
            : info.data?.ClusterAlias || cluster || "集群拓扑"
        }
        description={
          anonymize
            ? "主机名称已匿名显示"
            : `${cluster || "正在解析集群"} · ${instances.length} 个实例`
        }
        extra={
          <Space>
            <Link
              to={`/audit-recovery/cluster/${encodeURIComponent(cluster || "")}`}
            >
              恢复记录
            </Link>
            <Dropdown
              menu={{
                items: [
                  {
                    key: "alias",
                    label: "修改集群别名",
                    disabled: !config.authorizedForAction,
                  },
                  {
                    key: "ack",
                    label: "确认集群恢复记录",
                    disabled: !config.authorizedForAction,
                  },
                  { key: "pools", label: "查看资源池" },
                  { key: "osc", label: "在线变更候选副本" },
                ],
                onClick: ({ key }) => {
                  if (key === "alias")
                    operate(
                      "set-cluster-alias",
                      { cluster },
                      { alias: info.data?.ClusterAlias },
                    );
                  if (key === "ack") operate("ack-cluster", { cluster });
                  if (key === "pools")
                    navigate(
                      `/cluster-pools/${encodeURIComponent(cluster || "")}`,
                    );
                  if (key === "osc")
                    navigate(
                      `/search?s=${encodeURIComponent(cluster || "")}&osc=${encodeURIComponent(cluster || "")}`,
                    );
                },
              }}
            >
              <Button icon={<MoreOutlined />}>集群操作</Button>
            </Dropdown>
          </Space>
        }
      />
      <QueryError
        error={
          data.error ||
          maintenance.error ||
          info.error ||
          analyses.error ||
          recovery.error ||
          recent.error ||
          blocked.error ||
          pools.error
        }
        retry={refreshAll}
      />
      {recovery.data?.map((item) => (
        <Alert
          key={item.Id}
          type="warning"
          showIcon
          title="集群正在恢复，拓扑可能变化"
          description={
            <Link to={`/audit-recovery/id/${item.Id}`}>
              查看恢复 #{item.Id} · {item.AnalysisEntry.Analysis}
            </Link>
          }
        />
      ))}
      {analyses.data
        ?.filter(
          (item) =>
            item.Analysis !== "NoProblem" || item.StructureAnalysis?.length,
        )
        .map((item) => (
          <Alert
            key={instanceID(item.AnalyzedInstanceKey)}
            type={item.IsDowntimed ? "info" : "warning"}
            showIcon
            title={`${instanceID(item.AnalyzedInstanceKey)} · ${item.Analysis}`}
            description={[item.Description, ...(item.StructureAnalysis || [])]
              .filter(Boolean)
              .join(" · ")}
          />
        ))}
      {recent.data?.length && !recovery.data?.length ? (
        <Alert
          type="info"
          showIcon
          title="集群近期完成恢复"
          description={
            <Link to={`/audit-recovery/id/${recent.data[0].Id}`}>
              查看恢复结果 · {recent.data[0].RecoveryEndTimestamp}
            </Link>
          }
        />
      ) : null}
      {blocked.data?.map((item) => (
        <Alert
          key={instanceID(item.FailedInstanceKey)}
          type="error"
          showIcon
          title={`${item.Analysis} 恢复受阻`}
          description={
            <Link to={`/audit-recovery/id/${item.BlockingRecoveryId}`}>
              查看前次恢复 #{item.BlockingRecoveryId}
            </Link>
          }
        />
      ))}
      <Card className="topology-card" styles={{ body: { padding: 0 } }}>
        <Flex
          className="topology-toolbar"
          justify="space-between"
          gap={12}
          align="center"
          wrap
        >
          <Space wrap>
            <Segmented
              value={view}
              onChange={setView}
              options={[
                {
                  value: "topology",
                  label: "拓扑",
                  icon: <ApartmentOutlined />,
                },
                {
                  value: "list",
                  label: "实例列表",
                  icon: <UnorderedListOutlined />,
                },
              ]}
            />
            <Tag
              color={
                !data.data || data.error
                  ? "default"
                  : bad
                    ? "warning"
                    : "success"
              }
            >
              {!data.data || data.error
                ? "实例状态待确认"
                : bad
                  ? `${bad} 个实例需要关注`
                  : instances.length
                    ? "实例状态正常"
                    : "暂无实例"}
            </Tag>
            <Tag>
              自动主库恢复{" "}
              {!info.data || info.error
                ? "未知"
                : info.data.HasAutomatedMasterRecovery
                  ? "开启"
                  : "关闭"}
            </Tag>
          </Space>
          <Input.Search
            aria-label="搜索实例"
            placeholder="搜索并查看实例"
            style={{ width: 230 }}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              if (event.target.value) setView("list");
            }}
            allowClear
          />
        </Flex>
        {view === "topology" && (
          <Flex className="topology-options" gap={14} align="center" wrap>
            <Typography.Text type="secondary">调整方式</Typography.Text>
            <Select
              aria-label="拓扑调整方式"
              value={mode}
              onChange={setMode}
              style={{ width: 155 }}
              options={[
                { value: "smart", label: "智能模式" },
                { value: "classic", label: "位点模式" },
                { value: "gtid", label: "GTID 模式" },
                ...(config.pseudoGTIDEnabled
                  ? [{ value: "pseudo-gtid", label: "Pseudo-GTID" }]
                  : []),
              ]}
            />
            <Checkbox
              checked={colorize}
              onChange={(event) => setColorize(event.target.checked)}
            >
              机房着色
            </Checkbox>
            <Checkbox
              checked={compact}
              onChange={(event) => setCompact(event.target.checked)}
            >
              紧凑显示
            </Checkbox>
            <Checkbox
              checked={aliases}
              onChange={(event) => setAliases(event.target.checked)}
            >
              实例别名
            </Checkbox>
            <Checkbox
              checked={anonymize}
              onChange={(event) => setAnonymize(event.target.checked)}
            >
              匿名显示
            </Checkbox>
            <Checkbox
              checked={poolVisible}
              onChange={(event) => setPoolVisible(event.target.checked)}
            >
              资源池
            </Checkbox>
          </Flex>
        )}
        {data.loading && !data.data ? (
          <LoadingState />
        ) : !instances.length ? (
          <div className="empty-block">
            <NoData description="此集群暂无实例" />
          </div>
        ) : view === "topology" ? (
          <Topology
            maintenance={maintenance.data || []}
            instances={instances}
            onSelect={select}
            compact={compact}
            anonymize={anonymize}
            aliases={aliases}
            colorize={colorize}
            mode={mode}
            pools={poolMap}
          />
        ) : (
          <InstanceTable instances={filtered} onSelect={select} />
        )}
      </Card>
      <InstanceDrawer
        selected={selected?.Key}
        initial={selected}
        onClose={() => setSelected(undefined)}
      />
    </>
  );
}
