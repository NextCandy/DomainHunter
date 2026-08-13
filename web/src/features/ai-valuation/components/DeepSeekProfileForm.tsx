import { useEffect, useMemo, useState } from "react";
import { ErrorNotice, Pill, Spinner, useToast } from "../../../components/ui";
import { useAIProfiles } from "../hooks/useAIProfiles";
import type { AIProfile, AIProfileInput, ConnectionTestResult } from "../lib/valuation-types";

const DEEPSEEK_DEFAULTS: AIProfileInput = {
  name: "DeepSeek 官方 · V4 Flash",
  provider: "openai_compatible",
  enabled: true,
  is_default: true,
  base_url: "https://api.deepseek.com",
  model: "deepseek-v4-flash",
  thinking_type: "disabled",
  reasoning_effort: "low",
  timeout_seconds: 30,
  max_tokens: 900,
  concurrency: 1,
  daily_limit: 50,
  cache_ttl_hours: 24,
};

export interface DeepSeekProfileFormProps {
  profile?: AIProfile | null;
  onUnauthorized?: () => void;
  onSaved?: (profile: AIProfile) => void;
  onCancel?: () => void;
}

function inputFromProfile(profile?: AIProfile | null): AIProfileInput {
  if (!profile) return DEEPSEEK_DEFAULTS;
  return {
    name: profile.name,
    provider: profile.provider,
    enabled: profile.enabled,
    is_default: profile.is_default,
    base_url: profile.base_url,
    model: profile.model,
    thinking_type: profile.thinking_type,
    reasoning_effort: profile.reasoning_effort,
    timeout_seconds: profile.timeout_seconds,
    max_tokens: profile.max_tokens,
    concurrency: profile.concurrency,
    daily_limit: profile.daily_limit,
    cache_ttl_hours: profile.cache_ttl_hours,
  };
}

function validateBaseURL(baseURL: string): string | null {
  try {
    const url = new URL(baseURL.trim());
    if (url.protocol !== "https:") return "生产环境只接受 HTTPS Base URL；本地开发例外应由后端环境变量控制。";
    if (url.pathname.replace(/\/$/, "").endsWith("/chat/completions")) return "请填写 Base URL，不要包含 /chat/completions；后端会自动拼接该路径。";
    if (url.username || url.password || url.search || url.hash) return "Base URL 不能包含账号、参数或锚点。";
    return null;
  } catch {
    return "请输入合法的 https:// Base URL。";
  }
}

export function DeepSeekProfileForm({ profile, onUnauthorized, onSaved, onCancel }: DeepSeekProfileFormProps) {
  const toast = useToast();
  const controller = useAIProfiles({ onUnauthorized });
  const [form, setForm] = useState<AIProfileInput>(() => inputFromProfile(profile));
  const [apiKey, setApiKey] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null);

  useEffect(() => {
    setForm(inputFromProfile(profile));
    setApiKey("");
    setLocalError(null);
    setTestResult(null);
  }, [profile]);

  const baseURLError = useMemo(() => validateBaseURL(form.base_url), [form.base_url]);
  const canSubmit = !baseURLError && form.name.trim().length > 0 && form.model.trim().length > 0 && !controller.saving;

  function update<K extends keyof AIProfileInput>(key: K, value: AIProfileInput[K]) {
    setForm((current) => ({ ...current, [key]: value }));
    setLocalError(null);
  }

  async function testConnection() {
    if (baseURLError) {
      setLocalError(baseURLError);
      return;
    }
    const result = await controller.testConnection({
      provider: form.provider,
      base_url: form.base_url,
      model: form.model,
      api_key: apiKey || undefined,
      thinking_type: form.thinking_type,
      reasoning_effort: form.reasoning_effort,
      timeout_seconds: form.timeout_seconds,
    });
    setTestResult(result);
    if (result?.ok) toast("连接测试成功", "success");
  }

  async function save() {
    if (baseURLError) {
      setLocalError(baseURLError);
      return;
    }
    const saved = await controller.save({ ...form, api_key: apiKey || undefined }, profile?.id);
    if (saved) {
      setApiKey("");
      toast("AI 配置已保存", "success");
      onSaved?.(saved);
    }
  }

  return (
    <section className="overflow-hidden rounded-card border border-line bg-surface">
      <header className="border-b border-line bg-surface-subtle px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div><span className="mono text-[11px] font-semibold tracking-[0.12em] text-accent">AI PROVIDER PROFILE</span><h2 className="mt-1 text-[16px] font-semibold text-ink">{profile ? "编辑 AI 估价档案" : "新建 AI 估价档案"}</h2><p className="mt-1 text-[12px] leading-5 text-ink-muted">默认使用 DeepSeek 官方 OpenAI-compatible 接口；密钥从不由读取接口回显，留空表示保留已保存的密钥或继续使用部署环境变量。</p></div>
          {profile && <Pill>{profile.api_key_set ? `Key：${profile.api_key_source === "environment" ? "环境变量" : "已加密保存"}` : "尚未设置 Key"}</Pill>}
        </div>
      </header>
      <div className="space-y-5 p-5">
        {(localError || controller.error) && <ErrorNotice message={localError || controller.error || "配置错误"} />}

        <section aria-labelledby="ai-connection-heading">
          <SectionHeading id="ai-connection-heading" title="连接" hint="服务端出站连接与档案身份" />
          <div className="mt-3 grid gap-4 md:grid-cols-2">
          <Field label="档案名称" hint="用于详情页和批量入队时识别。"><input className="input" value={form.name} maxLength={80} onChange={(event) => update("name", event.target.value)} /></Field>
          <Field label="提供商" hint="默认配置使用 OpenAI-compatible 协议。"><select className="input" value={form.provider} onChange={(event) => update("provider", event.target.value as AIProfileInput["provider"])}><option value="deepseek">DeepSeek 官方</option><option value="openai_compatible">OpenAI Compatible</option></select></Field>
          <Field label="Base URL" hint="填写服务根地址；不要填写 /chat/completions。"><input className="input mono" value={form.base_url} spellCheck={false} inputMode="url" onChange={(event) => update("base_url", event.target.value.trim())} aria-invalid={Boolean(baseURLError)} />{baseURLError && <p className="mt-1 text-[11px] text-danger">{baseURLError}</p>}</Field>
          </div>
        </section>

        <section aria-labelledby="ai-model-heading">
          <SectionHeading id="ai-model-heading" title="模型" hint="模型选择只影响研究性排序，不会改变域名状态或可注册结论" />
          <div className="mt-3 grid gap-4 md:grid-cols-3">
            <Field label="模型" hint="批量排序建议使用快速模型；少量重点域名再选择深度模型。"><input className="input mono" list="domainhunter-models" value={form.model} spellCheck={false} onChange={(event) => update("model", event.target.value.trim())} /><datalist id="domainhunter-models"><option value="deepseek-v4-flash" /><option value="deepseek-v4-pro" /></datalist></Field>
            <Field label="Thinking"><select className="input" value={form.thinking_type} onChange={(event) => update("thinking_type", event.target.value as AIProfileInput["thinking_type"])}><option value="disabled">关闭（默认批量）</option><option value="enabled">启用（重点域名）</option></select></Field>
            <Field label="推理强度"><select className="input" value={form.reasoning_effort} onChange={(event) => update("reasoning_effort", event.target.value as AIProfileInput["reasoning_effort"])}><option value="low">low</option><option value="high">high</option><option value="max">max</option></select></Field>
          </div>
        </section>

        <section aria-labelledby="ai-cost-heading">
          <SectionHeading id="ai-cost-heading" title="成本与执行" hint="限制超时、并发、每日额度与缓存，避免无人值守时消耗失控" />
          <div className="mt-3 grid gap-4 md:grid-cols-4"><NumberField label="超时" suffix="秒" min={5} max={120} value={form.timeout_seconds} onChange={(value) => update("timeout_seconds", value)} /><NumberField label="最大输出" suffix="tokens" min={200} max={4096} value={form.max_tokens} onChange={(value) => update("max_tokens", value)} /><NumberField label="并发" suffix="任务" min={1} max={5} value={form.concurrency} onChange={(value) => update("concurrency", value)} /><NumberField label="每日额度" suffix="次" min={1} max={1000} value={form.daily_limit} onChange={(value) => update("daily_limit", value)} /></div>
          <div className="mt-4 max-w-[220px]"><Field label="缓存 TTL"><select className="input" value={form.cache_ttl_hours} onChange={(event) => update("cache_ttl_hours", Number(event.target.value))}><option value={6}>6 小时</option><option value={24}>24 小时（默认）</option><option value={72}>72 小时</option></select></Field></div>
        </section>

        <section aria-labelledby="ai-security-heading">
          <SectionHeading id="ai-security-heading" title="安全" hint="密钥只写不读；所有 Provider 请求由后端发起并执行 allowlist 与 SSRF 校验" />
          <div className="mt-3 border-y border-line py-4"><div className="flex items-center justify-between gap-3"><div><p className="text-[13px] font-semibold text-ink">API Key</p><p className="mt-1 text-[12px] leading-5 text-ink-muted">优先使用 <code className="mono">DOMAINHUNTER_AI_API_KEY</code>。若已配置应用级加密，可在此处安全写入 Key。</p></div><Pill title="任何 GET 接口均不返回完整 Key">只写不读</Pill></div><input className="input mono mt-3" type="password" autoComplete="new-password" placeholder={profile?.api_key_set ? "留空以保持当前 Key" : "sk-…（可选，或使用环境变量）"} value={apiKey} onChange={(event) => setApiKey(event.target.value)} /></div>
          <label className="mt-4 flex cursor-pointer items-start gap-3"><input className="mt-0.5 h-4 w-4 accent-accent" type="checkbox" checked={form.enabled} onChange={(event) => update("enabled", event.target.checked)} /><span><span className="text-[13px] font-medium text-ink">启用此档案</span><span className="mt-1 block text-[12px] leading-5 text-ink-muted">关闭后不会接收新的估价任务，历史结果仍可查看。</span></span></label>
          <label className="mt-3 flex cursor-pointer items-start gap-3"><input className="mt-0.5 h-4 w-4 accent-accent" type="checkbox" checked={form.is_default} onChange={(event) => update("is_default", event.target.checked)} /><span><span className="text-[13px] font-medium text-ink">设为默认档案</span><span className="mt-1 block text-[12px] leading-5 text-ink-muted">详情页“加入估价队列”会优先使用此档案；不覆盖已有任务的档案选择。</span></span></label>
        </section>

        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4"><div>{testResult && <TestResult result={testResult} />}</div><div className="flex flex-wrap gap-2"><button type="button" className="btn h-9" disabled={controller.saving || Boolean(baseURLError)} onClick={() => void testConnection()}>{controller.saving ? <Spinner /> : "测试连接"}</button>{onCancel && <button type="button" className="btn h-9" disabled={controller.saving} onClick={onCancel}>取消</button>}<button type="button" className="btn btn-primary h-9" disabled={!canSubmit} onClick={() => void save()}>{controller.saving ? <Spinner /> : "保存档案"}</button></div></div>
      </div>
    </section>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return <label className="block"><span className="text-[13px] font-medium text-ink">{label}</span>{hint && <span className="mt-1 block text-[11px] leading-4 text-ink-muted">{hint}</span>}<div className="mt-2">{children}</div></label>;
}

function SectionHeading({ id, title, hint }: { id: string; title: string; hint: string }) {
  return <div><h3 id={id} className="text-[13px] font-semibold text-ink">{title}</h3><p className="mt-1 text-[11px] leading-4 text-ink-muted">{hint}</p></div>;
}

function NumberField({ label, suffix, min, max, value, onChange }: { label: string; suffix: string; min: number; max: number; value: number; onChange: (value: number) => void }) {
  return <label className="block"><span className="text-[13px] font-medium text-ink">{label}</span><div className="relative mt-2"><input className="input pr-16" type="number" min={min} max={max} value={value} onChange={(event) => onChange(Math.max(min, Math.min(max, Number(event.target.value) || min)))} /><span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-[11px] text-ink-muted">{suffix}</span></div></label>;
}

function TestResult({ result }: { result: ConnectionTestResult }) {
  return <div className={result.ok ? "rounded-card border border-accent/30 bg-accent-soft/45 px-3 py-2 text-[12px] text-ink" : "rounded-card border border-line bg-surface-muted px-3 py-2 text-[12px] text-ink-muted"}><strong>{result.ok ? "连接成功" : "连接失败"}</strong><span className="ml-2">{result.message}</span>{result.ok && <span className="ml-2 mono">{result.latency_ms ?? "—"} ms · {result.model ?? "—"}</span>}</div>;
}
