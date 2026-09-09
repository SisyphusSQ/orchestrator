import { createContext, useContext } from "react";
import type { WebConfig } from "../api/types";
export const ConfigContext = createContext<WebConfig>({
  urlPrefix: "",
  userId: "",
  authorizedForAction: false,
  authorizedForConfiguration: false,
  agentsEnabled: false,
  pseudoGTIDEnabled: false,
  removeTextFromHostnameDisplay: "",
  webMessage: "",
  auditPageSize: 20,
  auditEnabled: false,
});
export const useConfig = () => useContext(ConfigContext);
