export type SSHKey = {
  id: string;
  name: string;
  algorithm: string;
  fingerprint: string;
  public_key: string;
};
export type Host = {
  id: string;
  name: string;
  address: string;
  port: number;
  username: string;
  auth: { mode: "key" | "password_prompt"; key_id?: string };
  trusted_host_key?: string;
  group?: string;
  remark?: string;
};
export type Snapshot = {
  revision: string;
  load_error: string;
  username: string;
  keys: SSHKey[];
  hosts: Host[];
  version: string;
};
export class APIError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}
export async function api<T = Snapshot>(
  path: string,
  method = "GET",
  data?: unknown,
  revision?: string,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    method,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(revision ? { "If-Match": `"${revision}"` } : {}),
    },
    body: data === undefined ? undefined : JSON.stringify(data),
    signal,
  });
  let result;
  try {
    result = await response.json();
  } catch {
    throw new APIError("服务暂时不可用，请稍后重试", response.status);
  }
  if (!response.ok)
    throw new APIError(result.error || "请求失败", response.status);
  return result as T;
}
export const message = (error: unknown) =>
  error instanceof Error ? error.message : "操作失败";
