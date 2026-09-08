import { useState } from "react";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Space,
  Table,
  Typography,
} from "antd";
import { PlusOutlined, SearchOutlined } from "@ant-design/icons";
import { useLocation, useNavigate } from "react-router-dom";
import { endpoint } from "../api/client";
import { useQuery } from "../api/use-query";
import type { Instance, InstanceKey } from "../api/types";
import { useConfig } from "../app/context";
import { instanceID } from "../domain/instance";
import { InstanceDrawer } from "../components/instance-drawer";
import { InstanceTable } from "../components/instance-table";
import { InstanceLink, PageTitle, QueryError } from "../components/common";
import { useOperation } from "../components/operations";

export function SearchPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const params = new URLSearchParams(location.search);
  const term =
    params.get("s") ||
    decodeURIComponent(location.pathname.split("/")[2] || "");
  const osc = params.get("osc");
  const [search, setSearch] = useState(term);
  const [selected, setSelected] = useState<Instance>();
  const data = useQuery<Instance[]>(
    osc
      ? endpoint("cluster-osc-replicas", osc)
      : term
        ? endpoint("search", term)
        : null,
  );
  return (
    <>
      <PageTitle
        title={osc ? "在线变更候选副本" : "搜索实例"}
        description="按主机名、端口或实例信息检索已发现的 MySQL 实例。"
      />
      <Card>
        <Input.Search
          aria-label="实例搜索关键词"
          prefix={<SearchOutlined />}
          placeholder="例如 mysql、3306"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          onSearch={(value) =>
            navigate(`/search?s=${encodeURIComponent(value)}`)
          }
          enterButton="搜索"
          style={{ maxWidth: 550 }}
        />
      </Card>
      <QueryError error={data.error} retry={data.refresh} />
      {(term || osc) && (
        <Card className="section-gap">
          <InstanceTable
            instances={data.data || []}
            onSelect={setSelected}
            loading={data.loading && !data.data}
          />
        </Card>
      )}
      <InstanceDrawer
        selected={selected?.Key}
        initial={selected}
        onClose={() => setSelected(undefined)}
      />
    </>
  );
}
export function DiscoverPage() {
  const config = useConfig();
  const operate = useOperation();
  return (
    <>
      <PageTitle
        title="发现实例"
        description="添加一个 MySQL 实例，自动发现其上下游复制关系。"
      />
      <div className="discovery-grid">
        <Card title="实例连接地址">
          <Form
            layout="vertical"
            initialValues={{ host: "", port: 3306 }}
            onFinish={(value: { host: string; port: number }) =>
              operate(
                "discover",
                {},
                { targetHost: value.host.trim(), targetPort: value.port },
              )
            }
          >
            <Form.Item
              name="host"
              label="主机名或 IP"
              rules={[
                {
                  required: true,
                  whitespace: true,
                  message: "请输入主机名或 IP",
                },
              ]}
            >
              <Input placeholder="mysql-01.example.com" autoComplete="off" />
            </Form.Item>
            <Form.Item
              name="port"
              label="MySQL 端口"
              rules={[{ required: true, message: "请输入端口" }]}
            >
              <InputNumber
                min={1}
                max={65535}
                precision={0}
                style={{ width: "100%" }}
              />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              icon={<PlusOutlined />}
              disabled={!config.authorizedForAction}
            >
              发现实例
            </Button>
          </Form>
        </Card>
        <Card title="发现过程">
          <Typography.Paragraph>
            Orchestrator 会使用服务端配置的 MySQL
            账号探测此实例，沿复制关系查找主库与副本。
          </Typography.Paragraph>
          <Typography.Paragraph>
            发现可能需要一些时间。成功后可到集群总览查看拓扑；停止复制或暂时不可达的实例，可以单独添加。
          </Typography.Paragraph>
          <Alert
            type="info"
            showIcon
            title="连接权限由服务端管理"
            description="这里仅填写地址，无需在浏览器保存数据库账号或密码。"
          />
        </Card>
      </div>
    </>
  );
}
export function PoolsPage() {
  const location = useLocation();
  const cluster = decodeURIComponent(location.pathname.split("/")[2] || "");
  const data = useQuery<Record<string, InstanceKey[]>>(
    endpoint("cluster-pool-instances", cluster),
  );
  const config = useConfig();
  const operate = useOperation();
  const rows = Object.entries(data.data || {}).map(([name, instances]) => ({
    name,
    instances,
  }));
  return (
    <>
      <PageTitle
        title="资源池"
        description={`集群 ${cluster} 的实例分组。`}
        extra={
          <Button
            type="primary"
            disabled={!config.authorizedForAction}
            onClick={() => operate("submit-pool-instances")}
          >
            配置资源池
          </Button>
        }
      />
      <QueryError error={data.error} retry={data.refresh} />
      <Card>
        <Table
          rowKey="name"
          dataSource={rows}
          loading={data.loading && !data.data}
          columns={[
            { title: "资源池", dataIndex: "name", key: "name" },
            {
              title: "实例数量",
              key: "count",
              render: (_, row) => row.instances.length,
            },
            {
              title: "成员",
              key: "instances",
              render: (_, row) => (
                <Space wrap>
                  {row.instances.map((key) => (
                    <InstanceLink key={instanceID(key)} value={key} />
                  ))}
                </Space>
              ),
            },
            {
              title: "操作",
              key: "action",
              render: (_, row) => (
                <Button
                  disabled={!config.authorizedForAction}
                  onClick={() =>
                    operate(
                      "submit-pool-instances",
                      {},
                      {
                        pool: row.name,
                        instances: row.instances.map(instanceID).join(","),
                      },
                    )
                  }
                >
                  编辑
                </Button>
              ),
            },
          ]}
        />
      </Card>
    </>
  );
}
