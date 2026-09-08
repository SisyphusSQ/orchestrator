import type { Preview } from "@storybook/react-vite";
import { mswLoader } from "msw-storybook-addon/csf3";
import { setupWorker } from "msw/browser";
import { MemoryRouter } from "react-router-dom";
import { AppTheme } from "../src/app/theme";
import { ConfigContext } from "../src/app/context";
import { OperationProvider } from "../src/components/operations";
import { apiHandlers, webConfig } from "../src/stories/fixtures";
import "antd/dist/reset.css";
import "../src/styles.css";

const preview: Preview = {
  tags: ["autodocs"],
  loaders: [
    mswLoader(async () => {
      const worker = setupWorker(...apiHandlers);
      await worker.start({
        quiet: true,
        onUnhandledRequest(request, print) {
          if (new URL(request.url).pathname.startsWith("/api/")) print.error();
        },
      });
      return worker;
    }),
  ],
  decorators: [
    (Story, context) => (
      <AppTheme>
        <MemoryRouter
          key={context.id}
          initialEntries={[context.parameters.route || "/clusters"]}
        >
          <ConfigContext.Provider
            value={{ ...webConfig, ...context.parameters.webConfig }}
          >
            <OperationProvider>
              <div
                style={
                  context.parameters.layout === "fullscreen"
                    ? undefined
                    : { padding: 24 }
                }
              >
                <Story />
              </div>
            </OperationProvider>
          </ConfigContext.Provider>
        </MemoryRouter>
      </AppTheme>
    ),
  ],
  parameters: {
    layout: "padded",
    controls: { expanded: true },
    options: { storySort: { order: ["控制台", "业务组件", "操作流程"] } },
    docs: {
      description: {
        component:
          "复用 Web 控制台的真实组件、主题和状态逻辑。示例使用隔离的模拟 API，可直接体验交互。",
      },
    },
  },
};
export default preview;
