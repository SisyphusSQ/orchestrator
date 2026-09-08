import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Drawer,
  Dropdown,
  Space,
  Tabs,
  Tag,
  Typography,
} from "antd";
import { MoreOutlined } from "@ant-design/icons";
import { Link } from "react-router-dom";
import { useQuery } from "../api/use-query";
import { endpoint } from "../api/client";
import type {
  Instance,
  InstanceKey,
  Maintenance,
  Recovery,
} from "../api/types";
import { useConfig } from "../app/context";
import { actions } from "../domain/actions";
import { instanceID, lagText, replicationMode, role } from "../domain/instance";
import { clusterLink, InstanceTag, JsonDetails, QueryError } from "./common";
import { useOperation } from "./operations";

export function InstanceDrawer({
  selected,
  initial,
  onClose,
}: {
  selected?: InstanceKey;
  initial?: Instance;
  onClose: () => void;
}) {
  const { data, error, refresh } = useQuery<Instance>(
    selected ? endpoint("instance", selected.Hostname, selected.Port) : null,
  );
  const tags = useQuery<string[]>(
    selected ? endpoint("tags", selected.Hostname, selected.Port) : null,
  );
  const maintenance = useQuery<Maintenance[]>(selected ? "/maintenance" : null);
  const recent = useQuery<Recovery[]>(
    selected
      ? endpoint(
          "recently-active-instance-recovery",
          selected.Hostname,
          selected.Port,
        )
      : null,
  );
  const equivalents = useQuery<InstanceKey[]>(
    data?.MasterKey?.Hostname && data.ExecBinlogCoordinates
      ? endpoint(
          "master-equivalent",
          data.MasterKey.Hostname,
          data.MasterKey.Port,
          data.ExecBinlogCoordinates.LogFile,
          data.ExecBinlogCoordinates.LogPos,
        )
      : null,
    0,
  );
  const instance = data || initial;
  const config = useConfig();
  const operate = useOperation();
  const groups = ["实例管理", "拓扑调整", "恢复与切换", "高级操作"];
  const menu = groups.map((group) => ({
    key: group,
    label: group,
    children: actions
      .filter(
        (action) =>
          action.group === group &&
          (config.pseudoGTIDEnabled ||
            !["match-below", "match-replicas"].includes(action.id)),
      )
      .map((action) => ({
        key: action.id,
        label: action.label,
        danger: group === "高级操作",
      })),
  }));
  const activeMaintenance = maintenance.data?.find(
    (item) => instanceID(item.Key) === instanceID(selected),
  );
  const details = (rows: [string, unknown][]) =>
    rows.map(([label, children]) => ({
      key: label,
      label,
      children:
        children === undefined || children === null || children === ""
          ? "—"
          : typeof children === "boolean"
            ? children
              ? "是"
              : "否"
            : String(children),
    }));
  return (
    <Drawer
      open={!!selected}
      onClose={onClose}
      title={
        <Space>
          实例详情{" "}
          <Typography.Text type="secondary" className="mono">
            {instanceID(selected)}
          </Typography.Text>
        </Space>
      }
      size={640}
      destroyOnHidden
      extra={
        <Dropdown
          disabled={!config.authorizedForAction}
          menu={{
            items: menu,
            onClick: ({ key }) => operate(key, { instance: selected }),
          }}
        >
          <Button icon={<MoreOutlined />}>更多操作</Button>
        </Dropdown>
      }
    >
      <QueryError error={error || recent.error} retry={refresh} />
      {recent.data?.map((item) => (
        <Alert
          key={item.Id}
          type="info"
          showIcon
          title="实例近期参与恢复"
          description={
            <Link to={`/audit-recovery/id/${item.Id}`} onClick={onClose}>
              查看恢复 #{item.Id}
            </Link>
          }
        />
      ))}
      {instance && (
        <>
          <div className="instance-summary">
            <InstanceTag instance={instance} />
            <Tag color="blue">{role(instance)}</Tag>
            <Tag>{replicationMode(instance)}</Tag>
            <Typography.Title level={4}>
              {instance.InstanceAlias || instanceID(instance.Key)}
            </Typography.Title>
            <Link to={clusterLink(instance.ClusterName)} onClick={onClose}>
              所属集群 · {instance.ClusterName}
            </Link>
          </div>
          <Space className="drawer-actions" wrap>
            <Button
              onClick={() => operate("refresh", { instance: selected })}
              disabled={!config.authorizedForAction}
            >
              立即探测
            </Button>
            <Button
              onClick={() =>
                operate("begin-maintenance", { instance: selected })
              }
              disabled={!config.authorizedForAction}
            >
              开始维护
            </Button>
            <Link
              to={`/audit/instance/${encodeURIComponent(instance.Key.Hostname)}/${instance.Key.Port}`}
              onClick={onClose}
            >
              查看审计
            </Link>
          </Space>
          <Tabs
            items={[
              {
                key: "overview",
                label: "基本信息",
                children: (
                  <>
                    <Descriptions
                      column={1}
                      size="small"
                      bordered
                      items={details([
                        ["主机", instance.Key.Hostname],
                        ["端口", instance.Key.Port],
                        ["版本", instance.Version],
                        ["可写", !instance.ReadOnly],
                        ["机房", instance.DataCenter],
                        ["环境", instance.PhysicalEnvironment],
                        ["运行时间", `${instance.Uptime || 0} 秒`],
                        ["Binlog 格式", instance.Binlog_format],
                        ["记录 Binlog", instance.LogBinEnabled],
                        ["记录复制更新", instance.LogReplicationUpdatesEnabled],
                        ["晋升规则", instance.PromotionRule],
                      ])}
                    />
                    <Typography.Title level={5}>标签</Typography.Title>
                    <QueryError error={tags.error} retry={tags.refresh} />
                    {tags.data?.length ? (
                      tags.data.map((tag) => <Tag key={tag}>{tag}</Tag>)
                    ) : (
                      <Typography.Text type="secondary">
                        暂无标签
                      </Typography.Text>
                    )}
                  </>
                ),
              },
              {
                key: "replication",
                label: "复制与 GTID",
                children: (
                  <>
                    <Descriptions
                      column={1}
                      bordered
                      size="small"
                      items={details([
                        ["上游", instanceID(instance.MasterKey)],
                        ["复制延迟", lagText(instance)],
                        ["SQL 线程", instance.ReplicationSQLThreadRuning],
                        ["IO 线程", instance.ReplicationIOThreadRuning],
                        ["延迟配置", `${instance.SQLDelay || 0} 秒`],
                        ["半同步主库", instance.SemiSyncMasterEnabled],
                        ["半同步副本", instance.SemiSyncReplicaEnabled],
                        ["SQL 错误", instance.LastSQLError],
                        ["IO 错误", instance.LastIOError],
                      ])}
                    />
                    <Typography.Title level={5}>复制位点</Typography.Title>
                    <Descriptions
                      column={1}
                      size="small"
                      items={Object.entries({
                        本机: instance.SelfBinlogCoordinates,
                        已执行: instance.ExecBinlogCoordinates,
                        已读取: instance.ReadBinlogCoordinates,
                        中继日志: instance.RelaylogCoordinates,
                      }).map(([key, value]) => ({
                        key,
                        label: key,
                        children: (
                          <code>
                            {value ? `${value.LogFile}:${value.LogPos}` : "—"}
                          </code>
                        ),
                      }))}
                    />
                    <Collapse
                      items={[
                        {
                          key: "gtid",
                          label: "GTID 集合与异常事务",
                          children: (
                            <>
                              <Typography.Text>已执行集合</Typography.Text>
                              <pre className="json-details">
                                {instance.ExecutedGtidSet || "无"}
                              </pre>
                              <Typography.Text>异常 GTID</Typography.Text>
                              <pre className="json-details">
                                {instance.GtidErrant || "无"}
                              </pre>
                            </>
                          ),
                        },
                      ]}
                    />
                    <Typography.Title level={5}>等价上游</Typography.Title>
                    <QueryError
                      error={equivalents.error}
                      retry={equivalents.refresh}
                    />
                    <Space wrap>
                      {equivalents.data?.map((key) => (
                        <Button
                          key={instanceID(key)}
                          size="small"
                          disabled={!config.authorizedForAction}
                          onClick={() =>
                            operate(
                              "move-equivalent",
                              { instance: selected },
                              {
                                targetHost: key.Hostname,
                                targetPort: key.Port,
                              },
                            )
                          }
                        >
                          {instanceID(key)}
                        </Button>
                      ))}
                    </Space>
                  </>
                ),
              },
              {
                key: "maintenance",
                label: "维护与停机",
                children: (
                  <>
                    <QueryError
                      error={maintenance.error}
                      retry={maintenance.refresh}
                    />
                    <Descriptions
                      title="维护"
                      column={1}
                      bordered
                      size="small"
                      items={details([
                        ["状态", activeMaintenance ? "维护中" : "未维护"],
                        ["负责人", activeMaintenance?.Owner],
                        ["原因", activeMaintenance?.Reason],
                        ["开始时间", activeMaintenance?.BeginTimestamp],
                      ])}
                    />
                    <Descriptions
                      className="section-gap"
                      title="停机"
                      column={1}
                      bordered
                      size="small"
                      items={details([
                        ["状态", instance.IsDowntimed ? "停机中" : "未停机"],
                        ["负责人", instance.DowntimeOwner],
                        ["原因", instance.DowntimeReason],
                        ["结束时间", instance.DowntimeEndTimestamp],
                      ])}
                    />
                    <Space className="section-gap" wrap>
                      {[
                        "begin-maintenance",
                        "end-maintenance",
                        "begin-downtime",
                        "end-downtime",
                      ].map((id) => (
                        <Button
                          disabled={!config.authorizedForAction}
                          key={id}
                          onClick={() => operate(id, { instance: selected })}
                        >
                          {actions.find((action) => action.id === id)!.label}
                        </Button>
                      ))}
                    </Space>
                  </>
                ),
              },
              {
                key: "raw",
                label: "详细属性",
                children: <JsonDetails value={instance} />,
              },
            ]}
          />
        </>
      )}
    </Drawer>
  );
}
