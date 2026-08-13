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

  const response = await fetch(path, {
    ...init,
    headers,
    credentials: "same-origin",
  });
  if (response.status === 401) throw new UnauthorizedError();

  const text = await response.text();
  if (!response.ok) {
    let message = text.trim() || `请求失败 (${response.status})`;
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed.error === "string") message = parsed.error;
      else if (parsed && typeof parsed.message === "string")
        message = parsed.message;
    } catch {
      /* 后端错误响应是纯文本，直接使用 */
    }
    throw new ApiError(message, response.status);
  }
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

async function requestRaw<T>(
  path: string,
  body: BodyInit,
  contentType: string,
): Promise<T> {
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
      else if (parsed && typeof parsed.message === "string")
        message = parsed.message;
    } catch {
      /* 后端错误响应是纯文本，直接使用 */
    }
    throw new ApiError(message, response.status);
  }
  const disposition = response.headers.get("Content-Disposition") ?? "";
  const filename =
    disposition.match(/filename="([^"]+)"/)?.[1] ?? "domainhunter-domains";
  return { blob: await response.blob(), filename };
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
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
    request<T>(path, {
      method: "POST",
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: "PUT",
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, {
      method: "PATCH",
      body: body === undefined ? undefined : JSON.stringify(body),
    }),
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
      request<{
        status: string;
        queued: number;
        window_seconds: number;
        message: string;
      }>("/api/v2/domains/batch-retry-failed", {
        method: "POST",
        body: JSON.stringify({}),
      }),
    exportDomains: (format: "csv" | "json") =>
      requestDownload(
        `/api/v2/domains/export?format=${encodeURIComponent(format)}`,
      ),
    importDomains: (
      content: string,
      format: "csv" | "json",
      mode: ImportMode,
    ) =>
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
      request<{ status: string; id: number }>(`/api/v2/folders/${id}`, {
        method: "DELETE",
      }),
  },
  notifications: {
    markRead: (ids: number[], read: boolean) =>
      request<{ status: string; updated: number }>("/api/v2/notifications/read", {
        method: "PATCH",
        body: JSON.stringify({ ids, read }),
      }),
    preferences: {
      get: () => request<{ muted_types: string[] }>("/api/v2/notifications/preferences"),
      update: (mutedTypes: string[]) => request<{ status: string; muted_types: string[] }>("/api/v2/notifications/preferences", {
        method: "PUT",
        body: JSON.stringify({ muted_types: mutedTypes }),
      }),
    },
    rules: {
      list: () =>
        request<{ rules: NotificationRule[] }>("/api/v2/notifications/rules"),
      create: (rule: NotificationRuleInput) =>
        request<NotificationRule>("/api/v2/notifications/rules", {
          method: "POST",
          body: JSON.stringify(rule),
        }),
      update: (id: number, rule: NotificationRuleInput) =>
        request<{ status: string; id: number }>(
          `/api/v2/notifications/rules/${id}`,
          {
            method: "PUT",
            body: JSON.stringify({ ...rule, id }),
          },
        ),
      remove: (id: number) =>
        request<{ status: string; id: number }>(
          `/api/v2/notifications/rules/${id}`,
          {
            method: "DELETE",
          },
        ),
    },
    templates: {
      list: () =>
        request<{ templates: NotificationTemplate[] }>(
          "/api/v2/notifications/templates",
        ),
      create: (template: NotificationTemplateInput) =>
        request<NotificationTemplate>("/api/v2/notifications/templates", {
          method: "POST",
          body: JSON.stringify(template),
        }),
      update: (id: number, template: NotificationTemplateInput) =>
        request<{ status: string; id: number }>(
          `/api/v2/notifications/templates/${id}`,
          {
            method: "PUT",
            body: JSON.stringify({ ...template, id }),
          },
        ),
      remove: (id: number) =>
        request<{ status: string; id: number }>(
          `/api/v2/notifications/templates/${id}`,
          {
            method: "DELETE",
          },
        ),
    },
    digest: {
      get: () => request<NotificationDigest>("/api/v2/notifications/digest"),
      update: (digest: NotificationDigestInput) =>
        request<{ status: string; digest: NotificationDigest }>(
          "/api/v2/notifications/digest",
          {
            method: "PUT",
            body: JSON.stringify(digest),
          },
        ),
    },
  },
  tokens: {
    list: () => request<{ tokens: ApiToken[] }>("/api/v2/tokens"),
    create: (input: ApiTokenInput) =>
      request<ApiToken>("/api/v2/tokens", {
        method: "POST",
        body: JSON.stringify(input),
      }),
    revoke: (id: number) =>
      request<{ status: string; id: number }>(`/api/v2/tokens/${id}`, {
        method: "DELETE",
      }),
  },
  p1: {
    savedViews: {
      list: () => request<{ views: SavedView[] }>("/api/v2/saved-views"),
      create: (input: { name: string; filter: FilterNode; shared: boolean }) =>
        request<SavedView>("/api/v2/saved-views", {
          method: "POST",
          body: JSON.stringify(input),
        }),
      update: (
        id: number,
        input: { name: string; filter: FilterNode; shared: boolean },
      ) =>
        request<SavedView>(`/api/v2/saved-views/${id}`, {
          method: "PUT",
          body: JSON.stringify(input),
        }),
      remove: (id: number) =>
        request<{ status: string; id: number }>(`/api/v2/saved-views/${id}`, {
          method: "DELETE",
        }),
    },
    bulkPreview: (action: BulkAction) =>
      request<BulkPreview>("/api/v2/bulk-actions/preview", {
        method: "POST",
        body: JSON.stringify(action),
      }),
    bulkExecute: (action: BulkAction) =>
      request<{ status: string; updated: number; queued?: number }>(
        "/api/v2/bulk-actions",
        {
          method: "POST",
          body: JSON.stringify(action),
        },
      ),
    bulkAudits: () =>
      request<{ audits: BulkAudit[] }>("/api/v2/bulk-actions/audits?limit=100"),
    ai: {
      settings: () => request<AISettings>("/api/v2/ai/settings"),
      saveSettings: (input: AISettingsInput) =>
        request<AISettings>("/api/v2/ai/settings", {
          method: "PUT",
          body: JSON.stringify(input),
        }),
      providers: {
        list: () =>
          request<{ providers: AIProviderProfile[] }>("/api/v2/ai/providers"),
        create: (input: AISettingsInput) =>
          request<AIProviderProfile>("/api/v2/ai/providers", {
            method: "POST",
            body: JSON.stringify(input),
          }),
        remove: (id: number) =>
          request<{ status: string; id: number }>(
            `/api/v2/ai/providers/${id}`,
            { method: "DELETE" },
          ),
      },
      models: () => request<{ models: string[] }>("/api/v2/ai/models"),
      usage: () => request<AIUsage>("/api/v2/ai/usage"),
      jobs: () => request<{ jobs: AIJob[] }>("/api/v2/ai/jobs?limit=100"),
      enqueue: (domains: string[]) =>
        request<{ status: string; queued: number }>("/api/v2/ai/jobs", {
          method: "POST",
          body: JSON.stringify({ domains }),
        }),
      valuation: (domain: string) =>
        request<{ valuation: Valuation | null }>(
          `/api/v2/ai/valuations/${encodeURIComponent(domain)}`,
        ),
    },
    automation: {
      rules: () =>
        request<{ rules: AutomationRule[] }>("/api/v2/automation/rules"),
      createRule: (rule: AutomationRuleInput) =>
        request<AutomationRule>("/api/v2/automation/rules", {
          method: "POST",
          body: JSON.stringify(rule),
        }),
      updateRule: (id: number, rule: AutomationRuleInput) =>
        request<{ status: string; id: number }>(
          `/api/v2/automation/rules/${id}`,
          {
            method: "PUT",
            body: JSON.stringify({ ...rule, id }),
          },
        ),
      removeRule: (id: number) =>
        request<{ status: string; id: number }>(
          `/api/v2/automation/rules/${id}`,
          { method: "DELETE" },
        ),
      dryRun: (id: number, event: AutomationEvent) =>
        request<{ runs: AutomationRun[]; side_effects: boolean }>(
          `/api/v2/automation/rules/${id}/dry-run`,
          {
            method: "POST",
            body: JSON.stringify(event),
          },
        ),
      runs: () =>
        request<{ runs: AutomationRun[] }>("/api/v2/automation/runs?limit=100"),
    },
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
  priority?: number;
  folder_id?: number | null;
  folder?: string;
  cached?: boolean;
  review?: ReviewState;
  ai_quality_score?: number | null;
}

export type ReviewReason =
  | "query_error"
  | "unknown_status"
  | "low_confidence"
  | "drop_status_future_expiry"
  | "provider_conflict"
  | "stale_evidence";

export interface ReviewState {
  required: boolean;
  reasons: ReviewReason[];
  severity?: "info" | "warning" | "critical";
  explanation?: string;
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
  as_of?: string;
  requests: number;
  errors: number;
  error_rate?: number;
  avg_latency_ms: number;
  p50_latency_ms?: number;
  p95_latency_ms?: number;
  consecutive_failures?: number;
  state_reason?: string;
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
  review?: ReviewState;
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
  action_counts: {
    available: number;
    drop_window: number;
    renewal_risk: number;
    review: number;
  };
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
  monitor: {
    check_interval: number;
    concurrent_limit: number;
    timeout: number;
  };
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
  read_at?: string | null;
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

export interface FilterNode {
  version?: number;
  logic?: "and" | "or";
  conditions?: FilterNode[];
  field?: string;
  op?: string;
  value?: string | number | boolean | Array<string | number>;
}

export interface SavedView {
  id: number;
  name: string;
  filter: FilterNode;
  shared: boolean;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface BulkAction {
  type:
    "tag" | "priority" | "folder" | "notification" | "monitor" | "ai_valuation";
  domains?: string[];
  filter?: FilterNode;
  tag?: string;
  priority?: number;
  folder_id?: number | null;
  enabled?: boolean;
  notify?: boolean;
}

export interface BulkPreview {
  action_type: string;
  matched: number;
  samples: string[];
  task_count: number;
  cache_hits: number;
  warning?: string;
}

export interface BulkAudit {
  id: number;
  action_type: string;
  matched: number;
  task_count: number;
  result: Record<string, unknown>;
  created_at: string;
}

export interface AISettings {
  profile_id: number;
  profile_name: string;
  is_default: boolean;
  provider: string;
  base_url: string;
  model: string;
  api_key_set: boolean;
  key_source: string;
  timeout_seconds: number;
  concurrency: number;
  max_output_tokens: number;
  cache_ttl_seconds: number;
  enabled: boolean;
}

export interface AISettingsInput {
  profile_id?: number;
  name?: string;
  is_default?: boolean;
  provider: string;
  base_url: string;
  model: string;
  api_key?: string;
  timeout_seconds: number;
  concurrency: number;
  max_output_tokens: number;
  cache_ttl_seconds: number;
  enabled: boolean;
}

export interface AIProviderProfile {
  profile_id: number;
  profile_name: string;
  is_default: boolean;
  provider: string;
  base_url: string;
  model: string;
  api_key_set: boolean;
  key_source: string;
  timeout_seconds: number;
  concurrency: number;
  max_output_tokens: number;
  cache_ttl_seconds: number;
  enabled: boolean;
}

export interface AIUsage {
  date: string;
  used: number;
  queued: number;
  running: number;
  succeeded: number;
  failed: number;
}

export interface AIJob {
  id: number;
  domain: string;
  status: string;
  attempts: number;
  max_attempts: number;
  last_error?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
}

export interface Valuation {
  domain: string;
  provider: string;
  model: string;
  analysis_version: string;
  input_fingerprint: string;
  quality_score: number;
  liquidity_score: number;
  risk_level: "low" | "medium" | "high";
  value_low: number;
  value_high: number;
  confidence: "low" | "medium" | "high";
  strengths: string[];
  limitations: string[];
  data_gaps: string[];
  rationale: string;
  disclaimer: string;
  generated_at: string;
  expires_at: string;
}

export interface AutomationRule {
  id: number;
  name: string;
  enabled: boolean;
  dry_run: boolean;
  trigger: FilterNode;
  conditions: FilterNode;
  actions: Array<Record<string, string | number | boolean>>;
  cooldown_seconds: number;
  daily_run_cap: number;
  created_at: string;
  updated_at: string;
}

export type AutomationRuleInput = Omit<
  AutomationRule,
  "id" | "created_at" | "updated_at"
>;

export interface AutomationEvent {
  event_id: string;
  type: string;
  domain?: string;
}

export interface AutomationRun {
  id: number;
  rule_id: number;
  event_id: string;
  domain: string;
  status: string;
  dry_run: boolean;
  action_count: number;
  details: Record<string, unknown>;
  started_at: string;
  completed_at?: string;
}
