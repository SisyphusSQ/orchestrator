import {
  createContext,
  useContext,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Result,
  Select,
  Space,
} from "antd";
import { ApiError, execute } from "../api/client";
import { refreshAll } from "../api/use-query";
import { useConfig } from "../app/context";
import { getAction, type ActionContext, type Values } from "../domain/actions";
import { instanceID } from "../domain/instance";
import { JsonDetails } from "./common";

type Selection = { id: string; context: ActionContext; values?: Values };
const OperationContext = createContext<
  (id: string, context?: ActionContext, values?: Values) => void
>(() => {});
export const useOperation = () => useContext(OperationContext);

export function OperationProvider({ children }: { children: ReactNode }) {
  const config = useConfig();
  const [selected, setSelected] = useState<Selection>();
  const [running, setRunning] = useState(false);
  const pending = useRef(false);
  const [result, setResult] = useState<{
    ok: boolean;
    error?: ApiError;
    details?: unknown;
    message?: string;
  }>();
  const [form] = Form.useForm<Values>();
  const action = selected && getAction(selected.id);
  const open = (id: string, context: ActionContext = {}, values?: Values) => {
    if (pending.current) return;
    getAction(id);
    setResult(undefined);
    setSelected({ id, context, values });
  };
  const submit = async () => {
    if (pending.current || !selected || !action || !config.authorizedForAction)
      return;
    let values: Values;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }
    if (pending.current) return;
    pending.current = true;
    setRunning(true);
    try {
      const response = await execute(action.path(selected.context, values));
      setResult({
        ok: true,
        details: response.Details,
        message: response.Message,
      });
    } catch (error) {
      const failure =
        error instanceof ApiError
          ? error
          : new ApiError((error as Error).message);
      setResult({ ok: false, error: failure, details: failure.details });
    } finally {
      pending.current = false;
      setRunning(false);
      refreshAll();
    }
  };
  return (
    <OperationContext.Provider value={open}>
      {children}
      <Modal
        open={!!selected}
        title={action?.label}
        onCancel={() => !running && setSelected(undefined)}
        width={580}
        closable={!running}
        mask={{ closable: !running }}
        keyboard={!running}
        destroyOnHidden
        footer={
          result ? (
            <Space>
              <Button onClick={refreshAll}>重新读取状态</Button>
              <Button type="primary" onClick={() => setSelected(undefined)}>
                关闭
              </Button>
            </Space>
          ) : (
            <Space>
              <Button disabled={running} onClick={() => setSelected(undefined)}>
                取消
              </Button>
              <Button
                danger
                type="primary"
                loading={running}
                disabled={!config.authorizedForAction}
                onClick={submit}
              >
                确认执行
              </Button>
            </Space>
          )
        }
      >
        {result ? (
          <>
            <Result
              status={
                result.ok
                  ? "success"
                  : result.error?.uncertain
                    ? "warning"
                    : "error"
              }
              title={
                result.ok
                  ? "操作执行成功"
                  : result.error?.uncertain
                    ? "操作结果未知"
                    : "操作执行失败"
              }
              subTitle={
                result.ok
                  ? `${result.message || "已执行"}。已请求重新读取当前状态，请以最新数据为准。`
                  : result.error?.message
              }
            />
            {result.details !== undefined && (
              <Collapse
                items={[
                  {
                    key: "details",
                    label: "查看详细结果",
                    children: <JsonDetails value={result.details} />,
                  },
                ]}
              />
            )}
          </>
        ) : (
          <>
            <Alert
              type="warning"
              showIcon
              title="请核实操作对象"
              description={action?.description}
            />
            <Descriptions
              className="operation-target"
              column={1}
              size="small"
              items={[
                ...(selected?.context.instance
                  ? [
                      {
                        key: "instance",
                        label: "实例",
                        children: (
                          <code>{instanceID(selected.context.instance)}</code>
                        ),
                      },
                    ]
                  : []),
                ...(selected?.context.cluster
                  ? [
                      {
                        key: "cluster",
                        label: "集群",
                        children: selected.context.cluster,
                      },
                    ]
                  : []),
                ...(selected?.context.agent
                  ? [
                      {
                        key: "agent",
                        label: "主机",
                        children: selected.context.agent,
                      },
                    ]
                  : []),
                ...(selected?.context.recovery
                  ? [
                      {
                        key: "recovery",
                        label: "恢复记录",
                        children: String(selected.context.recovery),
                      },
                    ]
                  : []),
                ...(selected?.context.seed
                  ? [
                      {
                        key: "seed",
                        label: "恢复任务",
                        children: String(selected.context.seed),
                      },
                    ]
                  : []),
              ]}
            />
            {!config.authorizedForAction && (
              <Alert type="error" title="当前用户只读或集群暂不可写" showIcon />
            )}
            <Form
              form={form}
              key={JSON.stringify(selected)}
              initialValues={{
                ...Object.fromEntries(
                  (action?.fields || []).map((field) => [
                    field.name,
                    field.name === "owner" ? config.userId : field.initial,
                  ]),
                ),
                ...selected?.values,
              }}
              layout="vertical"
              disabled={running}
              preserve={false}
            >
              {action?.fields.map((field) => (
                <Form.Item
                  key={field.name}
                  name={field.name}
                  label={field.label}
                  rules={[
                    {
                      required: field.required,
                      type: field.type === "number" ? "number" : "string",
                      whitespace: field.type !== "number",
                      message: `请填写${field.label}`,
                    },
                    ...(field.name === "duration"
                      ? [
                          {
                            pattern: /^\d+[smhdw]$/,
                            message: "使用数字和时间单位，例如 30m、1h、2d",
                          },
                        ]
                      : []),
                  ]}
                >
                  {field.type === "number" ? (
                    <InputNumber
                      min={1}
                      max={65535}
                      precision={0}
                      style={{ width: "100%" }}
                    />
                  ) : field.type === "textarea" ? (
                    <Input.TextArea rows={3} />
                  ) : field.type === "select" ? (
                    <Select options={field.options} />
                  ) : (
                    <Input autoComplete="off" />
                  )}
                </Form.Item>
              ))}
            </Form>
          </>
        )}
      </Modal>
    </OperationContext.Provider>
  );
}
