const TOKEN_KEY = "grok_panel_token";

export function getToken(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem(TOKEN_KEY) || "";
}

export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

function errorMessage(data: unknown, fallback: string): string {
  if (data && typeof data === "object") {
    const rec = data as Record<string, unknown>;
    if (typeof rec.error === "string" && rec.error) return rec.error;
    if (typeof rec.message === "string" && rec.message) return rec.message;
  }
  return fallback;
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers || {});
  if (!headers.has("Content-Type") && init.body && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const tok = getToken();
  if (tok) headers.set("X-Panel-Token", tok);

  const res = await fetch(path, { ...init, headers });
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text) as unknown;
    } catch {
      data = { raw: text };
    }
  }
  if (!res.ok) {
    throw new ApiError(res.status, errorMessage(data, res.statusText || "request failed"));
  }
  return data as T;
}

export function tokenQuery(): string {
  const t = getToken();
  return t ? `?token=${encodeURIComponent(t)}` : "";
}

export type Health = {
  ok: boolean;
  service: string;
  auth: boolean;
  time: string;
};

export type RunStatus = {
  status?: string;
  run_id?: string;
  target?: number;
  done?: number;
  success?: number;
  fail?: number;
  fail_count?: number;
  sso_count?: number;
  oauth_count?: number;
  phase?: string;
  phase_detail?: string;
  pid?: number;
  log_path?: string;
  output_dir?: string;
  started_at?: string;
  updated_at?: string;
  error?: string;
  rate_per_min?: number;
  workers?: { s?: number; p?: number; c?: number; oauth?: number };
};

export type ClusterNode = {
  id: string;
  name: string;
  last_seen: string;
  online: boolean;
  busy: boolean;
  capacity: number;
  running_target: number;
  assigned: number;
  completed_total: number;
  last_error?: string;
  remote_addr?: string;
};
export type MasterLink = {
  url: string;
  ok: boolean;
  last_error?: string;
  last_ok?: string;
  last_assign: number;
  need: number;
  master_name?: string;
};

export type ClusterStatus = {
  role: string;
  node_id: string;
  node_name: string;
  public_token_set: boolean;
  status_password_set?: boolean;
  master_url: string;
  master_urls?: string;
  masters?: string[];
  master_links?: MasterLink[];
  heartbeat_sec: number;
  pool_target: number;
  assign_min: number;
  assign_max: number;
  auto_register: boolean;
  auto_upload: boolean;
  share_pool_list?: boolean;
  share_pool_pull?: boolean;
  slave_connected: boolean;
  slave_last_error?: string;
  slave_last_ok?: string;
  last_assign: number;
  need: number;
  pool: {
    healthy: number;
    rate_limited: number;
    dead: number;
    disabled: number;
    total: number;
    quota_estimate: number;
  };
  nodes: ClusterNode[];
};

export type PanelConfig = Record<string, string | number | boolean | null | undefined>;

export type PluginInfo = {
  id: string;
  name: string;
  version: string;
  description?: string;
  runtime: string;
  kind?: string[];
  enabled: boolean;
  source: string;
  path?: string;
  capabilities?: string[];
  artifact_kinds?: string[];
  status?: string;
};

export type ArtifactInfo = {
  id: string;
  plugin: string;
  kind: string;
  status: string;
  labels?: Record<string, string>;
  run_id?: string;
  created_at: string;
  updated_at?: string;
};

export type TavilyKeyInfo = {
  id: string;
  api_key: string;
  status: string;
  note?: string;
  success?: number;
  failure?: number;
  last_used_unix?: number;
  created_at?: string;
};
