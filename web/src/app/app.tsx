import { lazy, Suspense, useState } from "react";
import {
  Alert,
  Breadcrumb,
  Button,
  Drawer,
  Flex,
  Layout,
  Menu,
  Result,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from "antd";
import {
  ApartmentOutlined,
  AuditOutlined,
  CloudServerOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  FieldTimeOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  PlusCircleOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SafetyOutlined,
  SearchOutlined,
  SettingOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import {
  Link,
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { useQuery, refreshAll } from "../api/use-query";
import type { WebConfig } from "../api/types";
import { ConfigContext } from "./context";
import { OperationProvider, useOperation } from "../components/operations";
import { ClustersPage } from "../pages/clusters";
import { AnalysisPage } from "../pages/analysis";
import { AuditPage } from "../pages/audit";
import { SearchPage, DiscoverPage, PoolsPage } from "../pages/instances";
import { AgentPage, AgentsPage, SeedsPage } from "../pages/agents";
import { HelpPage, StatusPage } from "../pages/status";
import { RecoverySettingsPage } from "../pages/recovery-settings";
import { QueryError } from "../components/common";

const ClusterPage = lazy(() =>
  import("../pages/cluster").then((module) => ({
    default: module.ClusterPage,
  })),
);

function RecoverySwitch({ authorized }: { authorized: boolean }) {
  const state = useQuery<"enabled" | "disabled">("/check-global-recoveries");
  const operate = useOperation();
  return (
    <Space className="recovery-control">
      <SafetyOutlined />
      <span>自动恢复</span>
      <Switch
        aria-label="全局自动恢复"
        size="small"
        checked={state.data === "enabled"}
        loading={state.loading && !state.data}
        disabled={!authorized || !!state.error || state.data === undefined}
        onChange={(checked) =>
          operate(
            checked ? "enable-global-recoveries" : "disable-global-recoveries",
          )
        }
      />
      {state.error && <Tag color="warning">状态未知</Tag>}
    </Space>
  );
}
function AgentGate({
  enabled,
  children,
}: {
  enabled: boolean;
  children: React.ReactNode;
}) {
  return enabled ? (
    children
  ) : (
    <Result
      status="info"
      title="Agent 服务未启用"
      subTitle="启用服务端 Agent 配置后可使用主机、快照与数据恢复功能。"
    />
  );
}

export function Application() {
  const boot = useQuery<WebConfig>("/web-config");
  const location = useLocation();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const [mobile, setMobile] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  if (!boot.data)
    return (
      <div className="startup-screen">
        {boot.error ? (
          <Result
            status="error"
            title="无法连接 Orchestrator"
            subTitle={boot.error.message}
            extra={
              <Button type="primary" onClick={boot.refresh}>
                重新连接
              </Button>
            }
          />
        ) : (
          <Spin size="large" description="正在连接 Orchestrator…" />
        )}
      </div>
    );
  const config = {
    ...boot.data,
    authorizedForAction: boot.data.authorizedForAction && !boot.error,
  };
  const selected =
    location.pathname.startsWith("/cluster/") ||
    location.pathname.startsWith("/cluster-pools/")
      ? "clusters"
      : location.pathname.startsWith("/audit-recovery-steps")
        ? "audit-recovery"
        : location.pathname.startsWith("/seed-details")
          ? "seeds"
          : location.pathname.startsWith("/agent/")
            ? "agents"
            : location.pathname.split("/")[1] || "clusters";
  const items = [
    { key: "clusters", label: "集群总览", icon: <DashboardOutlined /> },
    { key: "clusters-analysis", label: "故障分析", icon: <WarningOutlined /> },
    { key: "search", label: "搜索实例", icon: <SearchOutlined /> },
    { key: "discover", label: "发现实例", icon: <PlusCircleOutlined /> },
    { type: "divider" as const },
    { key: "audit", label: "操作审计", icon: <AuditOutlined /> },
    {
      key: "audit-failure-detection",
      label: "故障检测",
      icon: <FieldTimeOutlined />,
    },
    { key: "audit-recovery", label: "恢复记录", icon: <SafetyOutlined /> },
    ...(config.agentsEnabled
      ? [
          { type: "divider" as const },
          { key: "agents", label: "Agents", icon: <CloudServerOutlined /> },
          { key: "seeds", label: "数据恢复任务", icon: <DatabaseOutlined /> },
        ]
      : []),
    { type: "divider" as const },
    { key: "recovery-settings", label: "恢复配置", icon: <SafetyOutlined /> },
    { key: "status", label: "系统状态", icon: <SettingOutlined /> },
    { key: "about", label: "使用帮助", icon: <QuestionCircleOutlined /> },
  ];
  const navigation = (
    <>
      <Link to="/clusters" className="brand">
        <span className="brand-icon">
          <ApartmentOutlined />
        </span>
        {(!collapsed || mobile) && (
          <span>
            Orchestrator<small>MYSQL CONTROL PLANE</small>
          </span>
        )}
      </Link>
      <Menu
        mode="inline"
        selectedKeys={[selected]}
        items={items}
        onClick={({ key }) => {
          navigate(`/${key}`);
          setMobileOpen(false);
        }}
      />
      <div className="sidebar-foot">
        {!collapsed && <>复制拓扑与高可用管理</>}
      </div>
    </>
  );
  const title =
    items.find((item) => "key" in item && item.key === selected)?.label ||
    "控制台";
  return (
    <ConfigContext.Provider value={config}>
      <OperationProvider>
        <Layout className="app-layout">
          <Layout.Sider
            className="sidebar"
            theme="light"
            width={220}
            collapsedWidth={72}
            collapsed={mobile ? true : collapsed}
            breakpoint="lg"
            onBreakpoint={(broken) => setMobile(broken)}
            style={mobile ? { display: "none" } : {}}
          >
            {navigation}
          </Layout.Sider>
          <Drawer
            open={mobileOpen}
            onClose={() => setMobileOpen(false)}
            placement="left"
            size={250}
            styles={{ body: { padding: 0 } }}
            title="导航"
          >
            {navigation}
          </Drawer>
          <Layout className="main-layout">
            <Layout.Header className="app-header">
              <Flex align="center" gap={14}>
                <Button
                  type="text"
                  aria-label={mobile ? "打开导航" : "折叠导航"}
                  icon={
                    collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />
                  }
                  onClick={() =>
                    mobile ? setMobileOpen(true) : setCollapsed(!collapsed)
                  }
                />
                <Breadcrumb items={[{ title: "控制台" }, { title }]} />
              </Flex>
              <Space size={18}>
                <RecoverySwitch authorized={config.authorizedForAction} />
                <span className="user-name">{config.userId || "当前会话"}</span>
                <Tag color={config.authorizedForAction ? "blue" : "default"}>
                  {config.authorizedForAction ? "可操作" : "只读"}
                </Tag>
              </Space>
            </Layout.Header>
            <Layout.Content className="app-content">
              <Flex
                className="workspace-bar"
                justify="space-between"
                align="center"
              >
                <Space size={8}>
                  <span
                    className={`state-dot ${boot.error ? "orange" : "green"}`}
                  />
                  <Typography.Text type="secondary">
                    {boot.error ? "连接异常" : "已连接 Orchestrator"}
                  </Typography.Text>
                </Space>
                <Space>
                  <Typography.Text type="secondary">
                    {boot.at
                      ? `更新于 ${new Date(boot.at).toLocaleTimeString("zh-CN")}`
                      : "等待数据"}
                  </Typography.Text>
                  <Button
                    size="small"
                    type="text"
                    icon={<ReloadOutlined />}
                    onClick={refreshAll}
                  >
                    刷新
                  </Button>
                </Space>
              </Flex>
              <QueryError error={boot.error} retry={boot.refresh} />
              {config.webMessage && (
                <Alert
                  className="site-message"
                  showIcon
                  type="info"
                  title={config.webMessage}
                />
              )}
              <Suspense fallback={<Spin description="正在加载拓扑…" />}>
                <Routes>
                  <Route
                    path="/"
                    element={<Navigate to="/clusters" replace />}
                  />
                  <Route path="/clusters" element={<ClustersPage />} />
                  <Route
                    path="/cluster/*"
                    element={<ClusterPage key={location.pathname} />}
                  />
                  <Route path="/clusters-analysis" element={<AnalysisPage />} />
                  <Route
                    path="/search/*"
                    element={<SearchPage key={location.search} />}
                  />
                  <Route path="/discover" element={<DiscoverPage />} />
                  <Route path="/cluster-pools/*" element={<PoolsPage />} />
                  <Route
                    path="/audit/*"
                    element={<AuditPage key="audit" kind="audit" />}
                  />
                  <Route
                    path="/audit-recovery/*"
                    element={<AuditPage key="recovery" kind="audit-recovery" />}
                  />
                  <Route
                    path="/audit-recovery-steps/*"
                    element={<AuditPage key="steps" kind="audit-recovery" />}
                  />
                  <Route
                    path="/audit-failure-detection/*"
                    element={
                      <AuditPage
                        key="detection"
                        kind="audit-failure-detection"
                      />
                    }
                  />
                  <Route
                    path="/agents"
                    element={
                      <AgentGate enabled={config.agentsEnabled}>
                        <AgentsPage />
                      </AgentGate>
                    }
                  />
                  <Route
                    path="/agent/*"
                    element={
                      <AgentGate enabled={config.agentsEnabled}>
                        <AgentPage />
                      </AgentGate>
                    }
                  />
                  <Route
                    path="/seeds"
                    element={
                      <AgentGate enabled={config.agentsEnabled}>
                        <SeedsPage />
                      </AgentGate>
                    }
                  />
                  <Route
                    path="/seed-details/*"
                    element={
                      <AgentGate enabled={config.agentsEnabled}>
                        <SeedsPage />
                      </AgentGate>
                    }
                  />
                  <Route path="/status" element={<StatusPage />} />
                  <Route
                    path="/recovery-settings"
                    element={<RecoverySettingsPage />}
                  />
                  {["about", "home", "faq", "keep-calm"].map((path) => (
                    <Route
                      key={path}
                      path={`/${path}`}
                      element={<HelpPage />}
                    />
                  ))}
                  <Route
                    path="*"
                    element={
                      <Result
                        status="404"
                        title="页面不存在"
                        extra={<Link to="/clusters">返回集群总览</Link>}
                      />
                    }
                  />
                </Routes>
              </Suspense>
              <footer className="app-footer">
                Orchestrator · MySQL 复制与高可用
              </footer>
            </Layout.Content>
          </Layout>
        </Layout>
      </OperationProvider>
    </ConfigContext.Provider>
  );
}
