import { useState } from "react";
import { api, UnauthorizedError } from "../lib/api";
import type { ApiToken, SettingsV2 } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, ConfirmDialog, ErrorNotice, Pill, Spinner, useToast } from "../components/ui";
import { formatDateTime } from "../lib/format";

const RAW_MODES = [
  { value: "change_only", label: "仅在状态变化时保存" },
  { value: "always", label: "总是保存" },
  { value: "never", label: "不保存" },
];

const LOG_LEVELS = ["debug", "info", "warn", "error"];

export function SettingsPage({
  onUnauthorized,
  onCredentialsChanged,
}: {
  onUnauthorized: () => void;
  onCredentialsChanged: () => void;
}) {
  const { data, error, loading, reload } = useAsync<SettingsV2>(
    () => api.get<SettingsV2>("/api/v2/settings"),
    [],
    onUnauthorized,
  );

  if (loading && !data) {
    return (
      <div className="flex items-center gap-2 py-16 text-ink-muted">
        <Spinner /> 加载中…
      </div>
    );
  }
  if (error) return <ErrorNotice message={error} onRetry={reload} />;
  if (!data) return null;

  return (
    <div className="space-y-4">
      <header className="workspace-header">
        <span className="workspace-kicker">SYSTEM SETTINGS</span>
        <h1 className="mt-1 text-[28px] font-semibold tracking-tight">系统设置</h1>
        <p className="text-[12px] text-ink-muted">DomainHunter {data.version}</p>
      </header>

      <div className="grid gap-4 xl:grid-cols-2">
        <MonitorCard settings={data} onSaved={reload} />
        <HistoryCard settings={data} onSaved={reload} />
        <AccountCard settings={data} onChanged={onCredentialsChanged} />
        <TokenManagementCard onUnauthorized={onUnauthorized} />
        <MaintenanceCard settings={data} onSaved={reload} />
      </div>
    </div>
  );
}

function MonitorCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState(settings.monitor);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  async function save() {
    setSaving(true);
    try {
      await api.post("/api/settings/monitor", form);
      toast("监控参数已保存并热重载", "success");
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card title="监控参数">
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label="检查间隔（秒）" hint="所有正常状态的域名共用这个间隔">
          <input
            className="input"
            type="number"
            min={5}
            aria-label="检查间隔（秒）"
            value={form.check_interval}
            onChange={(event) => setForm({ ...form, check_interval: Number(event.target.value) })}
          />
        </Field>
        <Field label="并发 worker" hint="1–1000">
          <input
            className="input"
            type="number"
            min={1}
            max={1000}
            aria-label="并发 worker"
            value={form.concurrent_limit}
            onChange={(event) => setForm({ ...form, concurrent_limit: Number(event.target.value) })}
          />
        </Field>
        <Field label="查询超时（秒）" hint="1–120">
          <input
            className="input"
            type="number"
            min={1}
            max={120}
            aria-label="查询超时（秒）"
            value={form.timeout}
            onChange={(event) => setForm({ ...form, timeout: Number(event.target.value) })}
          />
        </Field>
      </div>
      <div className="mt-4">
        <button type="button" className="btn btn-primary h-8" onClick={save} disabled={saving}>
          {saving && <Spinner />}保存
        </button>
      </div>
    </Card>
  );
}

function HistoryCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState(settings.history);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  async function save() {
    setSaving(true);
    try {
      await api.put("/api/v2/settings/history", form);
      toast("历史保留策略已保存", "success");
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card title="历史数据保留">
      <p className="mb-3 text-[12px] text-ink-muted">
        观测与查询尝试会长期累积。这里的上限保证树莓派 / NAS 上的数据库不会无限增长。
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="保留天数" hint="0 表示不按时间清理">
          <input
            className="input"
            type="number"
            min={0}
            aria-label="保留天数"
            value={form.retention_days}
            onChange={(event) => setForm({ ...form, retention_days: Number(event.target.value) })}
          />
        </Field>
        <Field label="每域名最多保留" hint="0 表示不限制">
          <input
            className="input"
            type="number"
            min={0}
            aria-label="每域名最多保留"
            value={form.max_per_domain}
            onChange={(event) => setForm({ ...form, max_per_domain: Number(event.target.value) })}
          />
        </Field>
        <Field
          label="心跳间隔（小时）"
          hint="状态没变化时两条观测的最小间隔；0 表示每次查询都记录（数据库会涨得很快）"
        >
          <input
            className="input"
            type="number"
            min={0}
            aria-label="心跳间隔（小时）"
            value={form.heartbeat_hours}
            onChange={(event) => setForm({ ...form, heartbeat_hours: Number(event.target.value) })}
          />
        </Field>
        <Field label="原始报文">
          <select
            className="input"
            aria-label="原始报文保存策略"
            value={form.raw_mode}
            onChange={(event) => setForm({ ...form, raw_mode: event.target.value })}
          >
            {RAW_MODES.map((mode) => (
              <option key={mode.value} value={mode.value}>
                {mode.label}
              </option>
            ))}
          </select>
        </Field>
        <Field label="单条报文上限（字节）">
          <input
            className="input"
            type="number"
            min={0}
            aria-label="单条报文上限（字节）"
            value={form.raw_max_bytes}
            onChange={(event) => setForm({ ...form, raw_max_bytes: Number(event.target.value) })}
          />
        </Field>
      </div>
      <div className="mt-4">
        <button type="button" className="btn btn-primary h-8" onClick={save} disabled={saving}>
          {saving && <Spinner />}保存
        </button>
      </div>
    </Card>
  );
}

function AccountCard({ settings, onChanged }: { settings: SettingsV2; onChanged: () => void }) {
  const [username, setUsername] = useState(settings.username);
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  async function saveUsername() {
    setBusy(true);
    try {
      await api.post("/api/update-username", { username });
      toast("用户名已更新", "success");
    } catch (err) {
      toast(err instanceof Error ? err.message : "更新失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function savePassword() {
    setBusy(true);
    try {
      await api.post("/api/change-password", { current_password: current, new_password: next });
      toast("密码已修改，请重新登录", "success");
      setCurrent("");
      setNext("");
      window.setTimeout(onChanged, 800);
    } catch (err) {
      toast(err instanceof Error ? err.message : "修改失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="账户">
      <div className="grid gap-3">
        <Field label="用户名">
          <div className="flex gap-2">
            <input
              className="input"
              aria-label="用户名"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
            <button type="button" className="btn shrink-0" onClick={saveUsername} disabled={busy}>
              更新
            </button>
          </div>
        </Field>

        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="当前密码">
            <input
              className="input"
            type="password"
            autoComplete="current-password"
            aria-label="当前密码"
            value={current}
              onChange={(event) => setCurrent(event.target.value)}
            />
          </Field>
          <Field label="新密码" hint="至少 6 位">
            <input
              className="input"
            type="password"
            autoComplete="new-password"
            aria-label="新密码"
            value={next}
              onChange={(event) => setNext(event.target.value)}
            />
          </Field>
        </div>

        <div>
          <button
            type="button"
            className="btn btn-primary h-8"
            onClick={savePassword}
            disabled={busy || !current || next.length < 6}
          >
            {busy && <Spinner />}修改密码
          </button>
        </div>
      </div>

      <div className="mt-4 space-y-1 border-t border-line pt-3 text-[12px] text-ink-muted">
        <div className="flex items-center gap-2">
          Cookie Secure <Pill>{settings.security.cookie_secure}</Pill>
          SameSite <Pill>{settings.security.cookie_same_site}</Pill>
        </div>
        <div className="flex items-center gap-2">
          CSRF 防护 <Pill>{settings.security.csrf_enabled ? "已启用" : "已关闭"}</Pill>
          CORS <Pill>{(settings.security.cors_origins ?? []).join(", ") || "仅同源"}</Pill>
        </div>
        <p className="text-ink-faint">
          这几项属于部署级配置，可用 DOMAINHUNTER_COOKIE_SECURE / _COOKIE_SAMESITE /
          _CORS_ORIGINS / _CSRF_ENABLED 环境变量覆盖。
        </p>
      </div>
    </Card>
  );
}

function TokenManagementCard({ onUnauthorized }: { onUnauthorized: () => void }) {
  const tokens = useAsync<{ tokens: ApiToken[] }>(() => api.tokens.list(), [], onUnauthorized);
  const toast = useToast();
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>(["read", "write"]);
  const [createdToken, setCreatedToken] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [pendingRevoke, setPendingRevoke] = useState<ApiToken | null>(null);

  function toggleScope(scope: string) {
    setScopes((current) => (current.includes(scope) ? current.filter((item) => item !== scope) : [...current, scope]));
  }

  async function createToken() {
    if (!name.trim() || scopes.length === 0) return;
    setBusy(true);
    try {
      const result = await api.tokens.create({ name: name.trim(), scopes });
      setCreatedToken(result.token ?? null);
      setName("");
      toast("API Token 已创建，请立即复制保存", "success");
      tokens.reload();
    } catch (error) {
      if (error instanceof UnauthorizedError) {
        onUnauthorized();
        return;
      }
      toast(error instanceof Error ? error.message : "创建 API Token 失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function revokeToken() {
    if (!pendingRevoke) return;
    setBusy(true);
    try {
      await api.tokens.revoke(pendingRevoke.id);
      setPendingRevoke(null);
      toast("API Token 已撤销", "success");
      tokens.reload();
    } catch (error) {
      if (error instanceof UnauthorizedError) {
        onUnauthorized();
        return;
      }
      toast(error instanceof Error ? error.message : "撤销 API Token 失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="API Token 管理">
      {tokens.error && <ErrorNotice message={tokens.error} onRetry={tokens.reload} />}
      <p className="mb-3 text-[12px] text-ink-muted">
        Token 只在创建成功时显示一次；数据库仅保存不可逆哈希，离开此提示后无法再次查看原文。
      </p>
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
        <label>
          <span className="label">名称</span>
          <input
            className="input"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="例如：自动化脚本"
            aria-label="API Token 名称"
          />
        </label>
        <fieldset>
          <legend className="label">Scope</legend>
          <div className="flex gap-3 pt-1.5 text-[13px]">
            {[
              ["read", "读取"],
              ["write", "写入"],
            ].map(([scope, label]) => (
              <label key={scope} className="flex items-center gap-1.5">
                <input type="checkbox" checked={scopes.includes(scope)} onChange={() => toggleScope(scope)} />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
      </div>
      <button
        type="button"
        className="btn btn-primary mt-3 h-8"
        onClick={() => void createToken()}
        disabled={busy || !name.trim() || scopes.length === 0}
        aria-label="创建 API Token"
        title="创建 API Token（只显示一次）"
      >
        {busy && <Spinner />}创建 Token
      </button>

      {createdToken && (
        <div className="mt-3 rounded-md border border-amber-300 bg-amber-50 p-3 dark:border-amber-500/40 dark:bg-amber-500/10" role="alert">
          <p className="text-[12px] font-medium text-amber-900 dark:text-amber-200">请立即复制，此 Token 只显示一次</p>
          <code className="mt-2 block break-all rounded bg-black/5 px-2 py-1.5 text-[12px] text-amber-950 dark:bg-black/20 dark:text-amber-100">{createdToken}</code>
          <button
            type="button"
            className="btn mt-2 h-7 px-2 text-[12px]"
            onClick={() => {
              void navigator.clipboard?.writeText(createdToken);
              toast("Token 已复制", "success");
            }}
            aria-label="复制新创建的 API Token"
            title="复制 Token"
          >
            复制 Token
          </button>
        </div>
      )}

      <div className="mt-4 border-t border-line pt-3">
        <h3 className="text-[12px] font-medium text-ink-muted">已创建 Token</h3>
        {tokens.loading && !tokens.data ? (
          <div className="flex items-center gap-2 py-4 text-[12px] text-ink-muted"><Spinner />加载 Token…</div>
        ) : !tokens.data?.tokens || tokens.data.tokens.length === 0 ? (
          <p className="py-3 text-[12px] text-ink-faint">暂无 API Token</p>
        ) : (
          <ul className="mt-2 divide-y divide-line rounded-md border border-line">
            {tokens.data.tokens.map((token) => (
              <li key={token.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-[12px]">
                <div className="min-w-0">
                  <p className="truncate font-medium">{token.name}</p>
                  <p className="mt-0.5 text-[11px] text-ink-faint">
                    scope: {token.scopes.join(", ")} · 创建于 {formatDateTime(token.created_at)}
                    {token.last_used_at ? ` · 最近使用 ${formatDateTime(token.last_used_at)}` : ""}
                  </p>
                </div>
                {token.revoked_at ? (
                  <Pill className="text-ink-faint">已撤销</Pill>
                ) : (
                  <button
                    type="button"
                    className="btn btn-danger h-7 px-2 text-[12px]"
                    onClick={() => setPendingRevoke(token)}
                    disabled={busy}
                    aria-label={`撤销 API Token ${token.name}`}
                    title={`撤销 ${token.name}`}
                  >
                    撤销
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
      <ConfirmDialog
        open={Boolean(pendingRevoke)}
        title="撤销 API Token"
        description={pendingRevoke ? `撤销“${pendingRevoke.name}”后，使用它的脚本会立即失效。` : ""}
        confirmText="撤销 Token"
        danger
        onConfirm={() => void revokeToken()}
        onCancel={() => setPendingRevoke(null)}
      />
    </Card>
  );
}

function MaintenanceCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [level, setLevel] = useState(settings.log_level);
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const backups = useAsync<{ backups: string[] | null }>(
    () => api.get<{ backups: string[] | null }>("/api/v2/backups"),
    [],
  );

  async function run(action: () => Promise<unknown>, successMessage: string) {
    setBusy(true);
    try {
      await action();
      toast(successMessage, "success");
      onSaved();
      backups.reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : "操作失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="维护">
      <div className="grid gap-3">
        <Field label="日志级别">
          <div className="flex gap-2">
            <select
              className="input"
              aria-label="日志级别"
              value={level}
              onChange={(event) => setLevel(event.target.value)}
            >
              {LOG_LEVELS.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="btn shrink-0"
              disabled={busy}
              onClick={() => run(() => api.put("/api/v2/settings/log-level", { level }), "日志级别已更新")}
            >
              应用
            </button>
          </div>
        </Field>

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className="btn h-8"
            disabled={busy}
            onClick={() => run(() => api.post("/api/v2/backups"), "已生成数据库备份")}
          >
            立即备份数据库
          </button>
          <button
            type="button"
            className="btn h-8"
            disabled={busy}
            onClick={() =>
              run(() => api.post("/api/database/clean-orphaned"), "孤立数据清理完成")
            }
          >
            清理孤立数据
          </button>
          <button
            type="button"
            className="btn h-8"
            disabled={busy}
            onClick={() => run(() => api.post("/api/monitor/reload"), "已刷新查询源配置")}
          >
            刷新查询源配置
          </button>
        </div>

        <div>
          <span className="label">最近的自动备份</span>
          {!backups.data?.backups || backups.data.backups.length === 0 ? (
            <p className="text-[12px] text-ink-faint">暂无备份文件</p>
          ) : (
            <ul className="mono space-y-0.5 text-ink-muted">
              {backups.data.backups.map((name) => (
                <li key={name}>{name}</li>
              ))}
            </ul>
          )}
          <p className="mt-1 text-[11px] text-ink-faint">
            备份位于 data/backups/，使用 SQLite VACUUM INTO 生成，只保留最近 5 份。
          </p>
        </div>
      </div>
    </Card>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <span className="label">{label}</span>
      {children}
      {hint && <p className="mt-1 text-[11px] text-ink-faint">{hint}</p>}
    </div>
  );
}
