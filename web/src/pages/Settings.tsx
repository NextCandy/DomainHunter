import { useState } from "react";
import { api } from "../lib/api";
import type { SettingsV2 } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, ErrorNotice, Pill, Spinner, useToast } from "../components/ui";

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
      <header>
        <h1 className="text-[18px] font-semibold tracking-tight">系统设置</h1>
        <p className="text-[12px] text-ink-muted">DomainHunter {data.version}</p>
      </header>

      <div className="grid gap-4 xl:grid-cols-2">
        <MonitorCard settings={data} onSaved={reload} />
        <HistoryCard settings={data} onSaved={reload} />
        <AccountCard settings={data} onChanged={onCredentialsChanged} />
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
            value={form.retention_days}
            onChange={(event) => setForm({ ...form, retention_days: Number(event.target.value) })}
          />
        </Field>
        <Field label="每域名最多保留" hint="0 表示不限制">
          <input
            className="input"
            type="number"
            min={0}
            value={form.max_per_domain}
            onChange={(event) => setForm({ ...form, max_per_domain: Number(event.target.value) })}
          />
        </Field>
        <Field label="原始报文">
          <select
            className="input"
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
              value={current}
              onChange={(event) => setCurrent(event.target.value)}
            />
          </Field>
          <Field label="新密码" hint="至少 6 位">
            <input
              className="input"
              type="password"
              autoComplete="new-password"
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
