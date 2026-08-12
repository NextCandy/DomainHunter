import { useEffect, useState } from "react";
import { api } from "../lib/api";
import type {
  DomainStatus,
  NotificationDigest,
  NotificationDigestInput,
  NotificationRecord,
  NotificationRule,
  NotificationRuleInput,
  NotificationTemplate,
  NotificationTemplateInput,
  SettingsV2,
} from "../lib/api";
import { useAsync } from "../lib/useAsync";
import {
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorNotice,
  Pill,
  Spinner,
  StatusBadge,
  cx,
  useToast,
} from "../components/ui";
import { formatDateTime } from "../lib/format";

type NotificationTab = "channels" | "rules" | "templates";

interface AsyncResource<T> {
  data: T | null;
  error: string;
  loading: boolean;
  reload: () => void;
}

const EMPTY_RULES: NotificationRule[] = [];
const EMPTY_TEMPLATES: NotificationTemplate[] = [];

export function NotificationsPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const toast = useToast();
  const settings = useAsync<SettingsV2>(() => api.get<SettingsV2>("/api/v2/settings"), [], onUnauthorized);
  const history = useAsync<{ notifications: NotificationRecord[] | null }>(
    () => api.get<{ notifications: NotificationRecord[] | null }>("/api/v2/notifications"),
    [],
    onUnauthorized,
  );
  const rules = useAsync<{ rules: NotificationRule[] }>(
    () => api.notifications.rules.list(),
    [],
    onUnauthorized,
  );
  const templates = useAsync<{ templates: NotificationTemplate[] }>(
    () => api.notifications.templates.list(),
    [],
    onUnauthorized,
  );
  const digest = useAsync<NotificationDigest>(() => api.notifications.digest.get(), [], onUnauthorized);
  const [tab, setTab] = useState<NotificationTab>("channels");

  return (
    <div className="space-y-4">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">NOTIFICATION CENTER</span>
          <h1>通知</h1>
          <p className="mt-1 text-[12px] text-ink-muted">
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

      <div className="flex gap-1 overflow-x-auto border-b border-line" role="tablist" aria-label="通知设置分类">
        {([
          ["channels", "渠道与历史"],
          ["rules", "规则与摘要"],
          ["templates", "模板"],
        ] as Array<[NotificationTab, string]>).map(([value, label]) => (
          <button
            key={value}
            type="button"
            role="tab"
            aria-selected={tab === value}
            aria-controls={`notification-panel-${value}`}
            className={cx(
              "-mb-px shrink-0 border-b-2 px-3 py-2 text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
              tab === value ? "border-accent font-medium text-ink" : "border-transparent text-ink-muted hover:text-ink",
            )}
            onClick={() => setTab(value)}
          >
            {label}
          </button>
        ))}
      </div>

      {settings.error && <ErrorNotice message={settings.error} onRetry={settings.reload} />}
      {settings.loading && !settings.data && (
        <div className="flex items-center gap-2 py-8 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      )}

      {tab === "channels" && (
        <div id="notification-panel-channels" className="space-y-4" role="tabpanel">
          {settings.data && (
            <div className="grid gap-4 xl:grid-cols-2">
              <EmailCard settings={settings.data} onSaved={settings.reload} />
              <TelegramCard settings={settings.data} onSaved={settings.reload} />
              <BarkCard settings={settings.data} onSaved={settings.reload} />
              <FeishuCard settings={settings.data} onSaved={settings.reload} />
              <WebhookCard settings={settings.data} onSaved={settings.reload} />
            </div>
          )}

          {history.error && <ErrorNotice message={history.error} onRetry={history.reload} />}
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
      )}
      {tab === "rules" && (
        <div id="notification-panel-rules" className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]" role="tabpanel">
          <RulesPanel resource={rules} />
          <DigestCard resource={digest} />
        </div>
      )}
      {tab === "templates" && (
        <div id="notification-panel-templates" role="tabpanel">
          <TemplatesPanel resource={templates} />
        </div>
      )}
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
    <label className="block">
      <span className="label">{label}</span>
      {children}
      {hint && <p className="mt-1 text-[11px] text-ink-faint">{hint}</p>}
    </label>
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

interface RuleForm {
  name: string;
  enabled: boolean;
  statuses: string;
  silence_start: string;
  silence_end: string;
  per_domain: boolean;
  digest_enabled: boolean;
}

function emptyRule(): RuleForm {
  return {
    name: "",
    enabled: true,
    statuses: "",
    silence_start: "",
    silence_end: "",
    per_domain: true,
    digest_enabled: false,
  };
}

function ruleToForm(rule: NotificationRule): RuleForm {
  return {
    name: rule.name,
    enabled: rule.enabled,
    statuses: rule.statuses.join(", "),
    silence_start: rule.silence_start,
    silence_end: rule.silence_end,
    per_domain: rule.per_domain,
    digest_enabled: rule.digest_enabled,
  };
}

function formToRule(form: RuleForm): NotificationRuleInput {
  return {
    ...form,
    statuses: form.statuses
      .split(/[,\s]+/)
      .map((value) => value.trim())
      .filter(Boolean),
  };
}

function RulesPanel({ resource }: { resource: AsyncResource<{ rules: NotificationRule[] }> }) {
  const toast = useToast();
  const rules = resource.data?.rules ?? EMPTY_RULES;
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [form, setForm] = useState<RuleForm>(emptyRule);
  const [busy, setBusy] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<NotificationRule | null>(null);

  useEffect(() => {
    if (selectedId === null) return;
    const selected = rules.find((rule) => rule.id === selectedId);
    if (selected) setForm(ruleToForm(selected));
  }, [rules, selectedId]);

  async function save() {
    if (!form.name.trim()) {
      toast("规则名称不能为空", "error");
      return;
    }
    setBusy(true);
    try {
      const payload = formToRule({ ...form, name: form.name.trim() });
      if (selectedId === null) {
        const created = await api.notifications.rules.create(payload);
        setSelectedId(created.id);
      } else {
        await api.notifications.rules.update(selectedId, payload);
      }
      toast("通知规则已保存", "success");
      resource.reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "保存规则失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!pendingDelete) return;
    setBusy(true);
    try {
      await api.notifications.rules.remove(pendingDelete.id);
      if (selectedId === pendingDelete.id) {
        setSelectedId(null);
        setForm(emptyRule());
      }
      setPendingDelete(null);
      toast("通知规则已删除", "success");
      resource.reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "删除规则失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      title="通知规则"
      action={
        <button
          type="button"
          className="btn btn-primary h-7 px-2 text-[12px]"
          onClick={() => {
            setSelectedId(null);
            setForm(emptyRule());
          }}
          aria-label="新建通知规则"
          title="新建通知规则"
        >
          新建规则
        </button>
      }
    >
      {resource.error && <ErrorNotice message={resource.error} onRetry={resource.reload} />}
      {resource.loading && !resource.data ? (
        <div className="flex items-center gap-2 py-6 text-[12px] text-ink-muted"><Spinner />加载规则…</div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(150px,0.35fr)_minmax(0,1fr)]">
          <div className="space-y-1" role="listbox" aria-label="通知规则列表">
            {rules.length === 0 ? (
              <p className="rounded-md border border-dashed border-line px-3 py-5 text-center text-[12px] text-ink-faint">暂无规则</p>
            ) : (
              rules.map((rule) => (
                <button
                  key={rule.id}
                  type="button"
                  role="option"
                  aria-selected={selectedId === rule.id}
                  className={cx(
                    "flex w-full items-center justify-between gap-2 rounded-md border px-2.5 py-2 text-left text-[12px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
                    selectedId === rule.id ? "border-accent bg-accent-soft" : "border-line hover:bg-surface-muted",
                  )}
                  onClick={() => setSelectedId(rule.id)}
                  title={`编辑通知规则 ${rule.name}`}
                >
                  <span className="min-w-0 truncate">{rule.name}</span>
                  <Pill>{rule.enabled ? "启用" : "停用"}</Pill>
                </button>
              ))
            )}
          </div>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="规则名称">
                <input className="input" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
              </Field>
              <Field label="状态匹配" hint="留空表示匹配所有状态；多个状态用逗号分隔">
                <input className="input" value={form.statuses} onChange={(event) => setForm({ ...form, statuses: event.target.value })} placeholder="available, registered" />
              </Field>
              <Field label="静默开始" hint="可留空，格式 HH:MM">
                <input className="input" type="time" value={form.silence_start} onChange={(event) => setForm({ ...form, silence_start: event.target.value })} />
              </Field>
              <Field label="静默结束" hint="可留空，格式 HH:MM">
                <input className="input" type="time" value={form.silence_end} onChange={(event) => setForm({ ...form, silence_end: event.target.value })} />
              </Field>
            </div>
            <div className="grid gap-2 sm:grid-cols-3">
              <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} />启用规则</label>
              <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={form.per_domain} onChange={(event) => setForm({ ...form, per_domain: event.target.checked })} />按域名去重</label>
              <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={form.digest_enabled} onChange={(event) => setForm({ ...form, digest_enabled: event.target.checked })} />进入摘要</label>
            </div>
            <div className="flex flex-wrap gap-2">
              <button type="button" className="btn btn-primary h-8" onClick={() => void save()} disabled={busy} aria-label="保存通知规则" title="保存通知规则">{busy && <Spinner />}保存规则</button>
              {selectedId !== null && (
                <button type="button" className="btn btn-danger h-8" onClick={() => setPendingDelete(rules.find((rule) => rule.id === selectedId) ?? null)} disabled={busy} aria-label="删除通知规则" title="删除通知规则">删除规则</button>
              )}
            </div>
          </div>
        </div>
      )}
      <ConfirmDialog
        open={Boolean(pendingDelete)}
        title="删除通知规则"
        description={pendingDelete ? `确定删除“${pendingDelete.name}”吗？` : ""}
        confirmText="删除规则"
        danger
        onConfirm={() => void remove()}
        onCancel={() => setPendingDelete(null)}
      />
    </Card>
  );
}

function DigestCard({ resource }: { resource: AsyncResource<NotificationDigest> }) {
  const toast = useToast();
  const [form, setForm] = useState<NotificationDigestInput>({ enabled: false, hour: 9, minute: 0 });
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (resource.data) {
      setForm({ enabled: resource.data.enabled, hour: resource.data.hour, minute: resource.data.minute });
    }
  }, [resource.data]);

  async function save() {
    setBusy(true);
    try {
      await api.notifications.digest.update(form);
      toast("通知摘要设置已保存", "success");
      resource.reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "保存摘要设置失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="通知摘要">
      {resource.error && <ErrorNotice message={resource.error} onRetry={resource.reload} />}
      {resource.loading && !resource.data ? (
        <div className="flex items-center gap-2 py-6 text-[12px] text-ink-muted"><Spinner />加载摘要设置…</div>
      ) : (
        <div className="space-y-3">
          <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} />启用定时摘要</label>
          <div className="grid grid-cols-2 gap-3">
            <Field label="小时"><input className="input" type="number" min={0} max={23} value={form.hour} onChange={(event) => setForm({ ...form, hour: Number(event.target.value) })} /></Field>
            <Field label="分钟"><input className="input" type="number" min={0} max={59} value={form.minute} onChange={(event) => setForm({ ...form, minute: Number(event.target.value) })} /></Field>
          </div>
          {resource.data?.last_sent_at && <p className="text-[11px] text-ink-faint">上次发送：{formatDateTime(resource.data.last_sent_at)}</p>}
          <button type="button" className="btn btn-primary h-8" onClick={() => void save()} disabled={busy} aria-label="保存通知摘要设置" title="保存通知摘要设置">{busy && <Spinner />}保存摘要</button>
        </div>
      )}
    </Card>
  );
}

interface TemplateForm {
  name: string;
  event_type: string;
  subject: string;
  body: string;
  enabled: boolean;
}

function emptyTemplate(): TemplateForm {
  return { name: "", event_type: "status_change", subject: "", body: "", enabled: true };
}

function templateToForm(template: NotificationTemplate): TemplateForm {
  return {
    name: template.name,
    event_type: template.event_type,
    subject: template.subject,
    body: template.body,
    enabled: template.enabled,
  };
}

function TemplatesPanel({ resource }: { resource: AsyncResource<{ templates: NotificationTemplate[] }> }) {
  const toast = useToast();
  const templates = resource.data?.templates ?? EMPTY_TEMPLATES;
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [form, setForm] = useState<TemplateForm>(emptyTemplate);
  const [busy, setBusy] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<NotificationTemplate | null>(null);

  useEffect(() => {
    if (selectedId === null) return;
    const selected = templates.find((template) => template.id === selectedId);
    if (selected) setForm(templateToForm(selected));
  }, [templates, selectedId]);

  async function save() {
    if (!form.name.trim()) {
      toast("模板名称不能为空", "error");
      return;
    }
    setBusy(true);
    try {
      const payload: NotificationTemplateInput = { ...form, name: form.name.trim() };
      if (selectedId === null) {
        const created = await api.notifications.templates.create(payload);
        setSelectedId(created.id);
      } else {
        await api.notifications.templates.update(selectedId, payload);
      }
      toast("通知模板已保存", "success");
      resource.reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "保存模板失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!pendingDelete) return;
    setBusy(true);
    try {
      await api.notifications.templates.remove(pendingDelete.id);
      if (selectedId === pendingDelete.id) {
        setSelectedId(null);
        setForm(emptyTemplate());
      }
      setPendingDelete(null);
      toast("通知模板已删除", "success");
      resource.reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "删除模板失败", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      title="通知模板"
      action={<button type="button" className="btn btn-primary h-7 px-2 text-[12px]" onClick={() => { setSelectedId(null); setForm(emptyTemplate()); }} aria-label="新建通知模板" title="新建通知模板">新建模板</button>}
    >
      {resource.error && <ErrorNotice message={resource.error} onRetry={resource.reload} />}
      {resource.loading && !resource.data ? (
        <div className="flex items-center gap-2 py-6 text-[12px] text-ink-muted"><Spinner />加载模板…</div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-[minmax(180px,0.3fr)_minmax(0,1fr)]">
          <div className="space-y-1" role="listbox" aria-label="通知模板列表">
            {templates.length === 0 ? (
              <p className="rounded-md border border-dashed border-line px-3 py-5 text-center text-[12px] text-ink-faint">暂无模板</p>
            ) : templates.map((template) => (
              <button
                key={template.id}
                type="button"
                role="option"
                aria-selected={selectedId === template.id}
                className={cx("flex w-full items-center justify-between gap-2 rounded-md border px-2.5 py-2 text-left text-[12px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent", selectedId === template.id ? "border-accent bg-accent-soft" : "border-line hover:bg-surface-muted")}
                onClick={() => setSelectedId(template.id)}
                title={`编辑通知模板 ${template.name}`}
              >
                <span className="min-w-0 truncate">{template.name}</span>
                <Pill>{template.enabled ? "启用" : "停用"}</Pill>
              </button>
            ))}
          </div>
          <div className="space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="模板名称"><input className="input" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></Field>
              <Field label="事件类型" hint="例如 status_change"><input className="input" value={form.event_type} onChange={(event) => setForm({ ...form, event_type: event.target.value })} /></Field>
              <Field label="主题"><input className="input" value={form.subject} onChange={(event) => setForm({ ...form, subject: event.target.value })} /></Field>
            </div>
            <Field label="正文"><textarea className="input min-h-40 resize-y font-mono text-[12px]" value={form.body} onChange={(event) => setForm({ ...form, body: event.target.value })} /></Field>
            <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} />启用模板</label>
            <div className="flex flex-wrap gap-2">
              <button type="button" className="btn btn-primary h-8" onClick={() => void save()} disabled={busy} aria-label="保存通知模板" title="保存通知模板">{busy && <Spinner />}保存模板</button>
              {selectedId !== null && <button type="button" className="btn btn-danger h-8" onClick={() => setPendingDelete(templates.find((template) => template.id === selectedId) ?? null)} disabled={busy} aria-label="删除通知模板" title="删除通知模板">删除模板</button>}
            </div>
          </div>
        </div>
      )}
      <ConfirmDialog
        open={Boolean(pendingDelete)}
        title="删除通知模板"
        description={pendingDelete ? `确定删除“${pendingDelete.name}”吗？` : ""}
        confirmText="删除模板"
        danger
        onConfirm={() => void remove()}
        onCancel={() => setPendingDelete(null)}
      />
    </Card>
  );
}
