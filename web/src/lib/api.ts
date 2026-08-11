/**
 * 与后端交互的薄封装。
 *
 * - 所有写操作自动带上 CSRF 令牌（后端 double-submit 校验）
 * - 401 统一抛出 UnauthorizedError，由 App 切换到登录页
 */

export class ApiError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export class UnauthorizedError extends ApiError {
  constructor() {
    super("未登录或会话已过期", 401);
  }
}

let csrfToken = "";

export function setCsrfToken(token: string) {
  csrfToken = token || "";
}

function readCookie(name: string): string {
  const match = document.cookie.match(new RegExp("(^| )" + name + "=([^;]+)"));
  return match ? decodeURIComponent(match[2]) : "";
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method || "GET").toUpperCase();
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && method !== "HEAD") {
    const token = csrfToken || readCookie("csrf_token");
    if (token) headers.set("X-CSRF-Token", token);
  }

  const response = await fetch(path, { ...init, headers, credentials: "same-origin" });
  if (response.status === 401) throw new UnauthorizedError();

  const text = await response.text();
  if (!response.ok) {
    let message = text.trim() || `请求失败 (${response.status})`;
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === "string") message = parsed.error;
      else if (parsed && typeof parsed.message === "string") message = parsed.message;
    } catch {
      /* 后端错误响应是纯文本，直接使用 */
    }
    throw new ApiError(message, response.status);
  }
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

async function requestRaw<T>(path: string, body: BodyInit, contentType: string): Promise<T> {
  return request<T>(path, {
    method: "POST",
    body,
    headers: { "Content-Type": contentType },
  });
}

async function requestDownload(path: string): Promise<ApiDownload> {
  const response = await fetch(path, { credentials: "same-origin" });
  if (response.status === 401) throw new UnauthorizedError();
  if (!response.ok) {
    const text = await response.text();
    let message = text.trim() || `请求失败 (${response.status})`;
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === "string") message = parsed.error;
      else if (parsed && typeof parsed.message === "string") message = parsed.message;
    } catch {
      /* 后端错误响应是纯文本，直接使用 */
    }
    throw new ApiError(message, response.status);
  }
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename = disposition.match(/filename="([^"]+)"/)?.[1] ?? "domainhunter-domains";
  return { blob: await response.blob(), filename };
}

export interface ApiDownload {
  blob: Blob;
  filename: string;
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  getOptional: async <T>(path: string): Promise<T | null> => {
    try {
      return await request<T>(path);
    } catch (error) {
      // 新版接口尚未部署时，调用方可以继续使用旧接口数据；认证失效仍需交给 App 处理。
      if (error instanceof UnauthorizedError) throw error;
      return null;
    }
  },
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "POST", body: body === undefined ? undefined : JSON.stringify(body) }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "PUT", body: body === undefined ? undefined : JSON.stringify(body) }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: "PATCH", body: body === undefined ? undefined : JSON.stringify(body) }),
  delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
  domains: {
    batchMoveFolder: (domains: string[], folderId: number | null) =>
      request<{ status: string; moved: number; folder_id: number | null }>(
        "/api/v2/domains/batch-move-folder",
        {
          method: "POST",
          body: JSON.stringify({ domains, folder_id: folderId }),
        },
      ),
    batchRetryFailed: () =>
      request<{ status: string; queued: number; window_seconds: number; message: string }>(
        "/api/v2/domains/batch-retry-failed",
        { method: "POST", body: JSON.stringify({}) },
      ),
    exportDomains: (format: "csv" | "json") =>
      requestDownload(`/api/v2/domains/export?format=${encodeURIComponent(format)}`),
    importDomains: (content: string, format: "csv" | "json", mode: ImportMode) =>
      requestRaw<{ status: string; result: DomainImportResult }>(
        `/api/v2/domains/import?format=${format}&mode=${mode}`,
        content,
        format === "json" ? "application/json" : "text/csv",
      ),
  },
  folders: {
    list: () => request<FolderListResponse>("/api/v2/folders"),
    create: (name: string, parentId: number | null) =>
      request<Folder>("/api/v2/folders", {
        method: "POST",
        body: JSON.stringify({ name, parent_id: parentId }),
      }),
    update: (id: number, name: string, parentId: number | null) =>
      request<{ status: string; id: number }>(`/api/v2/folders/${id}`, {
        method: "PUT",
        body: JSON.stringify({ name, parent_id: parentId }),
      }),
    remove: (id: number) =>
      request<{ status: string; id: number }>(`/api/v2/folders/${id}`, { method: "DELETE" }),
  },
  notifications: {
    rules: {
      list: () => request<{ rules: NotificationRule[] }>("/api/v2/notifications/rules"),
      create: (rule: NotificationRuleInput) =>
        request<NotificationRule>("/api/v2/notifications/rules", {
          method: "POST",
          body: JSON.stringify(rule),
        }),
      update: (id: number, rule: NotificationRuleInput) =>
        request<{ status: string; id: number }>(`/api/v2/notifications/rules/${id}`, {
          method: "PUT",
          body: JSON.stringify({ ...rule, id }),
        }),
      remove: (id: number) =>
        request<{ status: string; id: number }>(`/api/v2/notifications/rules/${id}`, {
          method: "DELETE",
        }),
    },
    templates: {
      list: () => request<{ templates: NotificationTemplate[] }>("/api/v2/notifications/templates"),
      create: (template: NotificationTemplateInput) =>
        request<NotificationTemplate>("/api/v2/notifications/templates", {
          method: "POST",
          body: JSON.stringify(template),
        }),
      update: (id: number, template: NotificationTemplateInput) =>
        request<{ status: string; id: number }>(`/api/v2/notifications/templates/${id}`, {
          method: "PUT",
          body: JSON.stringify({ ...template, id }),
        }),
      remove: (id: number) =>
        request<{ status: string; id: number }>(`/api/v2/notifications/templates/${id}`, {
          method: "DELETE",
        }),
    },
    digest: {
      get: () => request<NotificationDigest>("/api/v2/notifications/digest"),
      update: (digest: NotificationDigestInput) =>
        request<{ status: string; digest: NotificationDigest }>("/api/v2/notifications/digest", {
          method: "PUT",
          body: JSON.stringify(digest),
        }),
    },
  },
  tokens: {
    list: () => request<{ tokens: ApiToken[] }>("/api/v2/tokens"),
    create: (input: ApiTokenInput) =>
      request<ApiToken>("/api/v2/tokens", { method: "POST", body: JSON.stringify(input) }),
    revoke: (id: number) =>
      request<{ status: string; id: number }>(`/api/v2/tokens/${id}`, { method: "DELETE" }),
  },
};

// ---------- 类型 ----------

export type DomainStatus =
  | "available"
  | "registered"
  | "grace"
  | "redemption"
  | "pending_delete"
  | "expired"
  | "transfer_locked"
  | "hold"
  | "unknown"
  | "error"
  | "skipped";

export interface Evidence {
  provider: string;
  status: DomainStatus;
  confidence: string;
  latency_ms: number;
  error?: string;
  note?: string;
  queried_at: string;
}

export interface DomainInfo {
  name: string;
  status: DomainStatus;
  registrar: string;
  created_date: string | null;
  expiry_date: string | null;
  updated_date: string | null;
  name_servers: string[] | null;
  last_checked: string;
  query_method: string;
  error_message: string;
  added_at: string | null;
  whois_raw: string;
  confidence?: string;
  epp_statuses?: string[];
  evidence?: Evidence[];
  next_check_at?: string | null;
  favorite?: boolean;
  tags?: string[];
  note?: string;
  folder_id?: number | null;
  folder?: string;
  cached?: boolean;
}

export interface DomainListResult {
  domains: DomainInfo[] | null;
  total: number;
  total_filtered: number;
  page: number;
  limit: number;
  total_pages: number;
  has_next: boolean;
  has_prev: boolean;
  data_status: string;
}

export interface Observation {
  id: number;
  domain: string;
  status: DomainStatus;
  registrar: string;
  registered_at: string | null;
  updated_at: string | null;
  expiry_at: string | null;
  name_servers: string[] | null;
  provider: string;
  confidence: string;
  observed_at: string;
  changed: boolean;
}

export interface Attempt {
  id: number;
  domain: string;
  observation_id: number;
  provider: string;
  status: DomainStatus;
  success: boolean;
  latency_ms: number;
  error_message: string;
  raw_response?: string;
  queried_at: string;
}

export interface ProviderHealth {
  provider: string;
  state: "healthy" | "degraded" | "offline" | "unknown";
  requests: number;
  errors: number;
  avg_latency_ms: number;
  last_error?: string;
  last_success?: string;
  last_failure?: string;
}

export interface OverviewItem {
  domain: string;
  status: DomainStatus;
  registrar?: string;
  expiry_at?: string;
  provider?: string;
  observed_at?: string;
  message?: string;
}

export interface Overview {
  total: number;
  status_counts: Record<string, number>;
  recent_changes: OverviewItem[] | null;
  upcoming_expiry: OverviewItem[] | null;
  recent_available: OverviewItem[] | null;
  query_failures: OverviewItem[] | null;
  providers: ProviderHealth[] | null;
  monitor: Record<string, unknown>;
  history?: { observations: number; attempts: number };
}

export interface OverviewTrendPoint {
  day: string;
  total: number;
  available: number;
  high_score: number;
  changes: number;
}

export interface SessionInfo {
  authenticated: boolean;
  auth_required: boolean;
  username?: string;
  csrf_token?: string;
  password_hashed?: boolean;
  version?: string;
}

export interface FacetItem {
  value: string;
  count: number;
}

export interface Facets {
  total: number;
  tlds: FacetItem[] | null;
  registrars: FacetItem[] | null;
  providers: FacetItem[] | null;
  statuses: FacetItem[] | null;
  tags: FacetItem[] | null;
}

export interface BarkSettings {
  url: string;
  group: string;
  sound: string;
  level: string;
  icon: string;
  enabled: boolean;
}

export interface FeishuSettings {
  webhook: string;
  secret_set: boolean;
  enabled: boolean;
}

export interface WebhookSettings {
  url: string;
  secret_set: boolean;
  enabled: boolean;
}

export interface SettingsV2 {
  smtp: {
    host: string;
    port: number;
    user: string;
    password_set: boolean;
    from: string;
    to: string;
    enabled: boolean;
  };
  telegram: { bot_token_set: boolean; chat_id: string; enabled: boolean };
  bark: BarkSettings;
  feishu: FeishuSettings;
  webhook: WebhookSettings;
  monitor: { check_interval: number; concurrent_limit: number; timeout: number };
  history: {
    retention_days: number;
    max_per_domain: number;
    heartbeat_hours: number;
    raw_mode: string;
    raw_max_bytes: number;
  };
  security: {
    cookie_secure: string;
    cookie_same_site: string;
    cors_origins: string[] | null;
    csrf_enabled: boolean;
  };
  log_level: string;
  query_policy: string;
  username: string;
  version: string;
}

export interface NotificationRecord {
  id: number;
  domain: string;
  status: string;
  old_status: string;
  sent_at: string;
  type: string;
}

export type ImportMode = "skip" | "overwrite" | "deduplicate";

export interface DomainImportResult {
  imported: number;
  overwritten: number;
  skipped: number;
  duplicates: number;
  invalid?: string[];
}

export interface Folder {
  id: number;
  name: string;
  parent_id?: number | null;
  created_at: string;
}

export interface FolderTreeNode extends Folder {
  children?: FolderTreeNode[] | null;
}

export interface FolderListResponse {
  folders: Folder[] | null;
  tree: FolderTreeNode[] | null;
}

export interface NotificationRule {
  id: number;
  name: string;
  enabled: boolean;
  statuses: string[];
  silence_start: string;
  silence_end: string;
  per_domain: boolean;
  digest_enabled: boolean;
}

export type NotificationRuleInput = Omit<NotificationRule, "id">;

export interface NotificationTemplate {
  id: number;
  name: string;
  event_type: string;
  subject: string;
  body: string;
  enabled: boolean;
}

export type NotificationTemplateInput = Omit<NotificationTemplate, "id">;

export interface NotificationDigest {
  enabled: boolean;
  hour: number;
  minute: number;
  last_sent_at?: string | null;
}

export type NotificationDigestInput = Omit<NotificationDigest, "last_sent_at">;

export interface ApiToken {
  id: number;
  name: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string | null;
  revoked_at?: string | null;
  token?: string;
}

export interface ApiTokenInput {
  name: string;
  scopes: string[];
}
