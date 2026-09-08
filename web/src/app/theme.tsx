import type { ReactNode } from "react";
import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";

export function AppTheme({ children }: { children: ReactNode }) {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#1677ff",
          colorBgLayout: "#f4f6f9",
          borderRadius: 6,
          fontFamily:
            'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif',
          fontSize: 13,
        },
        components: {
          Layout: {
            headerBg: "#ffffff",
            bodyBg: "#f4f6f9",
            siderBg: "#ffffff",
          },
          Menu: { itemHeight: 42, itemMarginInline: 12, itemBorderRadius: 6 },
          Table: { headerBg: "#f8fafc", cellPaddingBlock: 14 },
          Card: { paddingLG: 20 },
          Button: { primaryShadow: "none" },
        },
      }}
    >
      <App>{children}</App>
    </ConfigProvider>
  );
}
