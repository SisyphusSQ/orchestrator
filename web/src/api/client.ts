import type { Envelope } from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    public uncertain = false,
    public details?: unknown,
  ) {
    super(message);
  }
}

// /orchestrator/web/... and /web/... use the same compiled application.
export function inferPrefix(path: string): string {
  const index = path.indexOf("/web");
  return index >= 0 && (path[index + 4] === "/" || path.length === index + 4)
    ? path.slice(0, index)
    : "";
}
export const urlPrefix = inferPrefix(window.location.pathname);
export function endpoint(...parts: (string | number)[]): string {
  return (
    "/" + parts.map((value) => encodeURIComponent(String(value))).join("/")
  );
}

async function request<T>(
  path: string,
  mutation: boolean,
  signal?: AbortSignal,
  body?: unknown,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${urlPrefix}/api${path}`, {
      method: mutation ? "POST" : "GET",
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      signal: signal
        ? AbortSignal.any([
            signal,
            AbortSignal.timeout(mutation ? 120000 : 30000),
          ])
        : AbortSignal.timeout(mutation ? 120000 : 30000),
      headers: {
        Accept: "application/json",
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (error) {
    if (
      !mutation &&
      error instanceof DOMException &&
      error.name === "AbortError"
    )
      throw error;
    throw new ApiError(
      mutation
        ? "连接中断，操作结果未知。请回读状态后再决定下一步。"
        : "无法连接服务端，请检查服务和登录状态。",
      mutation,
    );
  }
  let value: unknown;
  try {
    value = await response.json();
  } catch {
    throw new ApiError(
      mutation
        ? "未收到有效结果，操作结果未知，请先回读状态。"
        : `服务端响应无效（HTTP ${response.status}）`,
      mutation,
    );
  }
  if (value && typeof value === "object" && "Code" in value) {
    const envelope = value as Envelope<T>;
    if (envelope.Code === "ERROR")
      throw new ApiError(
        envelope.Message || "操作失败",
        envelope.ErrorClass === "indeterminate",
        envelope.Details,
      );
    if (envelope.Code !== "OK")
      throw new ApiError("服务端返回了未知的结果状态", mutation);
    if (!response.ok)
      throw new ApiError(
        `HTTP ${response.status}：${envelope.Message}`,
        mutation,
        envelope.Details,
      );
    return mutation ? (value as T) : envelope.Details;
  }
  if (!response.ok)
    throw new ApiError(`请求失败（HTTP ${response.status}）`, mutation);
  if (mutation)
    throw new ApiError(
      "操作响应缺少确认状态，结果未知，请先回读。",
      true,
      value,
    );
  return value as T;
}

// Callers explicitly choose query or action; HTTP GET does not imply read-only.
export const query = <T>(path: string, signal?: AbortSignal) =>
  request<T>(path, false, signal);
export const execute = <T = unknown>(path: string) =>
  request<Envelope<T>>(path, true);
export const executeJSON = <T = unknown>(path: string, body: unknown) =>
  request<Envelope<T>>(path, true, undefined, body);
