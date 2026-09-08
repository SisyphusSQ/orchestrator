import React from "react";
import ReactDOM from "react-dom/client";
import { AppTheme } from "./app/theme";
import { BrowserRouter } from "react-router-dom";
import { Application } from "./app/app";
import { urlPrefix } from "./api/client";
import "antd/dist/reset.css";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <AppTheme>
      <BrowserRouter basename={`${urlPrefix}/web`}>
        <Application />
      </BrowserRouter>
    </AppTheme>
  </React.StrictMode>,
);
