import { useState } from "react";
import { api } from "../lib/api";
import type { DomainStatus, NotificationRecord, SettingsV2 } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, EmptyState, ErrorNotice, Pill, Spinner, StatusBadge, useToast } from "../components/ui";
import { formatDateTime } from "../lib/format";

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
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">通知</h1>
          <p className="text-[12px] text-ink-muted">
            首次查询不通知；从查询失败恢复不通知；可注册结论证据不足时也不会通知
          </p>
        </div>
        <button
          type="button"
          className="btn h-8"
          onClick={async () => {
            try {
              const result = await api.post<{ message: string; status: string }>("/api/notification/test");
              toast(result.message, result.status === "error" ? "error" : "success");
            } catch (err) {
              toast(err instanceof Error ? err.message : "测试失败", "error");
            }
          }}
        >
          测试全部已启用渠道
        </button>
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
          <BarkCard settings={settings.data} onSaved={settings.reload} />
          <FeishuCard settings={settings.data} onSaved={settings.reload} />
          <WebhookCard settings={settings.data} onSaved={settings.reload} />
        </div>
      )}

      <Card title="通知历史" bodyClassName="p-0">
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

// ---------- 通用零件 ----------

function ChannelTitle({ label, enabled }: { label: string; enabled: boolean }) {
  return (
    <span className="flex items-center gap-2">
      {label}
      <Pill className={enabled ? "text-emerald-600 dark:text-emerald-400" : undefined}>
        {enabled ? "已启用" : "未启用"}
      </Pill>
    </span>
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

function Actions({
  channel,
  saving,
  onSave,
}: {
  channel: string;
  saving: boolean;
  onSave: () => void;
}) {
  const [testing, setTesting] = useState(false);
  const toast = useToast();

  return (
    <div className="mt-4 flex gap-2">
      <button type="button" className="btn btn-primary h-8" onClick={onSave} disabled={saving}>
        {saving && <Spinner />}保存
      </button>
      <button
        type="button"
        className="btn h-8"
        disabled={testing}
        onClick={async () => {
          setTesting(true);
          try {
            const result = await api.post<{ status: string; message: string }>(
              `/api/v2/notifications/test/${channel}`,
            );
            toast(result.message, result.status === "success" ? "success" : "error");
          } catch (err) {
            toast(err instanceof Error ? err.message : "测试失败", "error");
          } finally {
            setTesting(false);
          }
        }}
      >
        {testing && <Spinner />}发送测试
      </button>
    </div>
  );
}

function useSave(path: string, onSaved: () => void, successMessage: string) {
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  const save = async (body: unknown) => {
    setSaving(true);
    try {
      await api.post(path, body);
      toast(successMessage, "success");
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  };
  return { saving, save };
}

// ---------- 各渠道 ----------

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
  const { saving, save } = useSave("/api/settings/smtp", onSaved, "SMTP 设置已保存");

  return (
    <Card title={<ChannelTitle label="邮件通知" enabled={settings.smtp.enabled} />}>
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
      <Actions channel="email" saving={saving} onSave={() => save(form)} />
    </Card>
  );
}

function TelegramCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({
    bot_token: "",
    chat_id: settings.telegram.chat_id,
    enabled: settings.telegram.enabled,
  });
  const { saving, save } = useSave("/api/settings/telegram", onSaved, "Telegram 设置已保存");

  return (
    <Card title={<ChannelTitle label="Telegram 通知" enabled={settings.telegram.enabled} />}>
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
      <Actions channel="telegram" saving={saving} onSave={() => save(form)} />
    </Card>
  );
}

const BARK_LEVELS = [
  { value: "", label: "默认" },
  { value: "active", label: "立即亮屏 active" },
  { value: "timeSensitive", label: "时效性 timeSensitive" },
  { value: "passive", label: "静默 passive" },
];

function BarkCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({ ...settings.bark });
  const { saving, save } = useSave("/api/settings/bark", onSaved, "Bark 设置已保存");

  return (
    <Card title={<ChannelTitle label="Bark（iOS 推送）" enabled={settings.bark.enabled} />}>
      <div className="grid gap-3">
        <Field
          label="推送地址"
          hint="完整地址，已包含设备 key。官方为 https://api.day.app/你的key，自建服务器同理。"
        >
          <input
            className="input"
            placeholder="https://api.day.app/xxxxxxxx"
            value={form.url}
            onChange={(event) => setForm({ ...form, url: event.target.value })}
          />
        </Field>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="分组">
            <input
              className="input"
              placeholder="DomainHunter"
              value={form.group}
              onChange={(event) => setForm({ ...form, group: event.target.value })}
            />
          </Field>
          <Field label="铃声">
            <input
              className="input"
              placeholder="可留空"
              value={form.sound}
              onChange={(event) => setForm({ ...form, sound: event.target.value })}
            />
          </Field>
          <Field label="中断级别">
            <select
              className="input"
              value={form.level}
              onChange={(event) => setForm({ ...form, level: event.target.value })}
            >
              {BARK_LEVELS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field label="图标 URL" hint="可留空；填写后会作为推送图标显示">
          <input
            className="input"
            value={form.icon}
            onChange={(event) => setForm({ ...form, icon: event.target.value })}
          />
        </Field>
      </div>
      <label className="mt-3 flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
        />
        启用 Bark 通知
      </label>
      <Actions channel="bark" saving={saving} onSave={() => save(form)} />
    </Card>
  );
}

function FeishuCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({
    webhook: settings.feishu.webhook,
    secret: "",
    enabled: settings.feishu.enabled,
  });
  const { saving, save } = useSave("/api/settings/feishu", onSaved, "飞书设置已保存");

  return (
    <Card title={<ChannelTitle label="飞书机器人" enabled={settings.feishu.enabled} />}>
      <div className="grid gap-3">
        <Field
          label="Webhook 地址"
          hint="群设置 → 群机器人 → 添加自定义机器人，复制得到的 webhook 地址"
        >
          <input
            className="input"
            placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/…"
            value={form.webhook}
            onChange={(event) => setForm({ ...form, webhook: event.target.value })}
          />
        </Field>
        <Field
          label={settings.feishu.secret_set ? "签名密钥（留空保持不变）" : "签名密钥"}
          hint="仅当机器人安全设置里开启了「签名校验」时需要填写"
        >
          <input
            className="input"
            type="password"
            value={form.secret}
            onChange={(event) => setForm({ ...form, secret: event.target.value })}
          />
        </Field>
      </div>
      <label className="mt-3 flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
        />
        启用飞书通知
      </label>
      <Actions channel="feishu" saving={saving} onSave={() => save(form)} />
    </Card>
  );
}

function WebhookCard({ settings, onSaved }: { settings: SettingsV2; onSaved: () => void }) {
  const [form, setForm] = useState({
    url: settings.webhook.url,
    secret: "",
    enabled: settings.webhook.enabled,
  });
  const { saving, save } = useSave("/api/settings/webhook", onSaved, "Webhook 设置已保存");

  return (
    <Card title={<ChannelTitle label="自定义 Webhook" enabled={settings.webhook.enabled} />}>
      <div className="grid gap-3">
        <Field label="接收地址" hint="事件会以 JSON POST 过去，可对接 n8n、企业微信中转、自建脚本">
          <input
            className="input"
            placeholder="https://example.com/hooks/domainhunter"
            value={form.url}
            onChange={(event) => setForm({ ...form, url: event.target.value })}
          />
        </Field>
        <Field
          label={settings.webhook.secret_set ? "签名密钥（留空保持不变）" : "签名密钥"}
          hint="填写后请求会带 X-DomainHunter-Signature: sha256=… 头，便于接收端验签"
        >
          <input
            className="input"
            type="password"
            value={form.secret}
            onChange={(event) => setForm({ ...form, secret: event.target.value })}
          />
        </Field>
      </div>
      <label className="mt-3 flex items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
        />
        启用 Webhook 通知
      </label>
      <Actions channel="webhook" saving={saving} onSave={() => save(form)} />
    </Card>
  );
}
