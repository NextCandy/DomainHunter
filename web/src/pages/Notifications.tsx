import { useState } from "react";
import { api } from "../lib/api";
import type { NotificationRecord, SettingsV2 } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, EmptyState, ErrorNotice, Pill, Spinner, StatusBadge, useToast } from "../components/ui";
import { formatDateTime } from "../lib/format";
import type { DomainStatus } from "../lib/api";

export function NotificationsPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const toast = useToast();
  const settings = useAsync<SettingsV2>(() => api.get<SettingsV2>("/api/v2/settings"), [], onUnauthorized);
  const history = useAsync<{ notifications: NotificationRecord[] | null }>(
    () => api.get<{ notifications: NotificationRecord[] | null }>("/api/v2/notifications"),
    [],
    onUnauthorized,
  );

  return (
    <div className="space-y-4">
      <header>
        <h1 className="text-[18px] font-semibold tracking-tight">通知</h1>
        <p className="text-[12px] text-ink-muted">
          首次查询不通知；从查询失败恢复不通知；可注册结论证据不足时也不会通知
        </p>
      </header>

      {settings.error && <ErrorNotice message={settings.error} onRetry={settings.reload} />}
      {settings.loading && !settings.data && (
        <div className="flex items-center gap-2 py-8 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      )}

      {settings.data && (
        <div className="grid gap-4 xl:grid-cols-2">
          <EmailCard settings={settings.data} onSaved={settings.reload} />
          <TelegramCard settings={settings.data} onSaved={settings.reload} />
        </div>
      )}

      <Card
        title="通知历史"
        action={
          <button
            type="button"
            className="btn h-7 px-2 text-[12px]"
            onClick={async () => {
              try {
                const result = await api.post<{ message: string; status: string }>(
                  "/api/notification/test",
                );
                toast(result.message, result.status === "error" ? "error" : "success");
              } catch (err) {
                toast(err instanceof Error ? err.message : "测试失败", "error");
              }
            }}
          >
            发送测试通知
          </button>
        }
        bodyClassName="p-0"
      >
        {history.loading && !history.data ? (
          <div className="flex items-center gap-2 p-4 text-ink-muted">
            <Spinner /> 加载中…
          </div>
        ) : !history.data?.notifications || history.data.notifications.length === 0 ? (
          <EmptyState title="还没有发送过通知" />
        ) : (
          <div className="table-scroll">
            <table className="w-full min-w-[520px] text-left">
              <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
                <tr>
                  <th className="px-4 py-2 font-medium">时间</th>
                  <th className="px-4 py-2 font-medium">域名</th>
                  <th className="px-4 py-2 font-medium">状态变化</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line text-[13px]">
                {history.data.notifications.map((record) => (
                  <tr key={record.id}>
                    <td className="tabular whitespace-nowrap px-4 py-2 text-ink-muted">
                      {formatDateTime(record.sent_at)}
                    </td>
                    <td className="mono px-4 py-2">{record.domain}</td>
                    <td className="flex items-center gap-2 px-4 py-2">
                      {record.old_status && <StatusBadge status={record.old_status as DomainStatus} />}
                      <span className="text-ink-faint">→</span>
                      <StatusBadge status={record.status as DomainStatus} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

function EmailCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({
    host: settings.smtp.host,
    port: settings.smtp.port,
    user: settings.smtp.user,
    password: "",
    from: settings.smtp.from,
    to: settings.smtp.to,
    enabled: settings.smtp.enabled,
  });
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  async function save() {
    setSaving(true);
    try {
      await api.post("/api/settings/smtp", form);
      toast("SMTP 设置已保存", "success");
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          邮件通知 {settings.smtp.enabled ? <Pill>已启用</Pill> : <Pill>未启用</Pill>}
        </span>
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="SMTP 服务器">
          <input
            className="input"
            value={form.host}
            onChange={(event) => setForm({ ...form, host: event.target.value })}
          />
        </Field>
        <Field label="端口">
          <input
            className="input"
            type="number"
            value={form.port}
            onChange={(event) => setForm({ ...form, port: Number(event.target.value) })}
          />
        </Field>
        <Field label="用户名">
          <input
            className="input"
            value={form.user}
            onChange={(event) => setForm({ ...form, user: event.target.value })}
          />
        </Field>
        <Field label={settings.smtp.password_set ? "密码（留空保持不变）" : "密码"}>
          <input
            className="input"
            type="password"
            value={form.password}
            onChange={(event) => setForm({ ...form, password: event.target.value })}
          />
        </Field>
        <Field label="发件人">
          <input
            className="input"
            value={form.from}
            onChange={(event) => setForm({ ...form, from: event.target.value })}
          />
        </Field>
        <Field label="收件人">
          <input
            className="input"
            value={form.to}
            onChange={(event) => setForm({ ...form, to: event.target.value })}
          />
        </Field>
      </div>

      <label className="mt-3 flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
        />
        启用邮件通知
      </label>

      <div className="mt-4 flex gap-2">
        <button type="button" className="btn btn-primary h-8" onClick={save} disabled={saving}>
          {saving && <Spinner />}保存
        </button>
        <TestButton path="/api/test/email" label="发送测试邮件" />
      </div>
    </Card>
  );
}

function TelegramCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({
    bot_token: "",
    chat_id: settings.telegram.chat_id,
    enabled: settings.telegram.enabled,
  });
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  async function save() {
    setSaving(true);
    try {
      await api.post("/api/settings/telegram", form);
      toast("Telegram 设置已保存", "success");
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          Telegram 通知 {settings.telegram.enabled ? <Pill>已启用</Pill> : <Pill>未启用</Pill>}
        </span>
      }
    >
      <div className="grid gap-3">
        <Field label={settings.telegram.bot_token_set ? "Bot Token（留空保持不变）" : "Bot Token"}>
          <input
            className="input"
            type="password"
            value={form.bot_token}
            onChange={(event) => setForm({ ...form, bot_token: event.target.value })}
          />
        </Field>
        <Field label="Chat ID">
          <input
            className="input"
            value={form.chat_id}
            onChange={(event) => setForm({ ...form, chat_id: event.target.value })}
          />
        </Field>
      </div>

      <label className="mt-3 flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
        />
        启用 Telegram 通知
      </label>

      <div className="mt-4 flex gap-2">
        <button type="button" className="btn btn-primary h-8" onClick={save} disabled={saving}>
          {saving && <Spinner />}保存
        </button>
        <TestButton path="/api/test/telegram" label="发送测试消息" />
      </div>
    </Card>
  );
}

function TestButton({ path, label }: { path: string; label: string }) {
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  return (
    <button
      type="button"
      className="btn h-8"
      disabled={busy}
      onClick={async () => {
        setBusy(true);
        try {
          const result = await api.post<{ status: string; message: string }>(path);
          toast(result.message, result.status === "success" ? "success" : "error");
        } catch (err) {
          toast(err instanceof Error ? err.message : "测试失败", "error");
        } finally {
          setBusy(false);
        }
      }}
    >
      {busy && <Spinner />}
      {label}
    </button>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <span className="label">{label}</span>
      {children}
    </div>
  );
}
