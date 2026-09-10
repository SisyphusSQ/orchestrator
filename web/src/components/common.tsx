import type { ReactNode } from "react";
import {
  Alert,
  Button,
  Card,
  Empty,
  Flex,
  Skeleton,
  Tag,
  Typography,
} from "antd";
import { Link } from "react-router-dom";
import type { Instance, InstanceKey } from "../api/types";
import { instanceID, instanceState } from "../domain/instance";

export function PageTitle({
  title,
  description,
  extra,
}: {
  title: string;
  description?: string;
  extra?: ReactNode;
}) {
  return (
    <Flex
      className="page-title"
      align="center"
      justify="space-between"
      gap={16}
      wrap
    >
      <div>
        <Typography.Title level={3}>{title}</Typography.Title>
        {description && (
          <Typography.Text type="secondary">{description}</Typography.Text>
        )}
      </div>
      {extra}
    </Flex>
  );
}
export function QueryError({
  error,
  retry,
}: {
  error?: Error;
  retry?: () => void;
}) {
  return error ? (
    <Alert
      className="query-error"
      type="error"
      showIcon
      title="数据读取失败"
      description={error.message}
      action={
        retry && (
          <Button size="small" onClick={retry}>
            重试
          </Button>
        )
      }
    />
  ) : null;
}
export function LoadingState() {
  return (
    <Card>
      <Skeleton active paragraph={{ rows: 5 }} />
    </Card>
  );
}
export function NoData({
  description = "暂无数据",
  children,
}: {
  description?: string;
  children?: ReactNode;
}) {
  return <Empty description={description}>{children}</Empty>;
}
export function InstanceTag({ instance }: { instance: Instance }) {
  const state = instanceState(instance);
  return <Tag color={state.color}>{state.label}</Tag>;
}
export const clusterLink = (cluster: string) =>
  `/cluster/${encodeURIComponent(cluster)}`;
export function InstanceLink({ value }: { value?: InstanceKey }) {
  return value?.Hostname ? (
    <Link
      className="mono"
      to={`/cluster/instance/${encodeURIComponent(value.Hostname)}/${value.Port}`}
    >
      {instanceID(value)}
    </Link>
  ) : (
    <>—</>
  );
}
export function JsonDetails({ value }: { value: unknown }) {
  return (
    <pre className="json-details">
      {JSON.stringify(value, null, 2) ?? "无附加信息"}
    </pre>
  );
}
