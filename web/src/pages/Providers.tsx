import { useMemo, useState } from "react";
import { api } from "../lib/api";
import type { ProviderHealth } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, ErrorNotice, Pill, Spinner, cx, useToast } from "../components/ui";
import { formatDateTime, formatLatency, providerLabel } from "../lib/format";

interface ProvidersResponse {
  providers: ProviderHealth[] | null;
  policy: unknown;
  bootstrap: { attempted: boolean; ok: boolean; merged: number; error?: string; last_update?: string; source?: string };
}

const STATE_LABEL: Record<ProviderHealth["state"], string> = { healthy: "正常", degraded: "降级", offline: "离线", unknown: "暂无数据" };
const STATE_CLASS: Record<ProviderHealth["state"], string> = { healthy: "bg-success", degraded: "bg-warning", offline: "bg-danger", unknown: "bg-neutral" };
const STATE_EDGE: Record<ProviderHealth["state"], string> = { healthy: "border-l-success", degraded: "border-l-warning", offline: "border-l-danger", unknown: "border-l-neutral" };
const STATE_ORDER: Record<ProviderHealth["state"], number> = { offline: 0, degraded: 1, healthy: 2, unknown: 3 };
const DEFAULT_CHAIN = ["who_dat", "whois_domain_lookup", "vercel_who_dat", "rdap", "rdap_org", "ai_fallback"];
const POLICY_EXAMPLE = `{\n  "default": {\n    "providers": ["who_dat", "whois_domain_lookup", "vercel_who_dat", "rdap", "rdap_org", "ai_fallback"]\n  }\n}`;

export function ProvidersPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, loading, reload } = useAsync<ProvidersResponse>(() => api.get<ProvidersResponse>("/api/v2/providers"), [], onUnauthorized);
  const [policyText, setPolicyText] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);
  const toast = useToast();
  const currentPolicy = policyText ?? (data?.policy ? JSON.stringify(data.policy, null, 2) : "");
  const providers = useMemo(() => [...(data?.providers ?? [])].sort((a, b) => STATE_ORDER[a.state] - STATE_ORDER[b.state]), [data]);
  const byName = useMemo(() => new Map((data?.providers ?? []).map((provider) => [provider.provider, provider])), [data]);

  async function savePolicy(next = currentPolicy) {
    setSaving(true);
    try {
      await api.put("/api/v2/settings/query-policy", { policy: next.trim() });
      toast("查询策略已保存并生效", "success");
      setPolicyText(null);
      reload();
    } catch (err) { toast(err instanceof Error ? err.message : "保存失败", "error"); }
    finally { setSaving(false); }
  }

  async function testProvider(provider: string) {
    setTesting(provider);
    try {
      const sample = provider === "whois_domain_lookup" ? "daydream.im" : "example.com";
      await api.post(`/api/v2/domains/${encodeURIComponent(sample)}/check`);
      toast(`${providerLabel(provider)} 测试查询已完成`, "success");
      reload();
    } catch (err) { toast(err instanceof Error ? err.message : "测试失败", "error"); }
    finally { setTesting(null); }
  }

  function moveProvider(index: number, direction: -1 | 1) {
    const target = index + direction;
    if (target < 0 || target >= DEFAULT_CHAIN.length) return;
    const chain = [...DEFAULT_CHAIN];
    [chain[index], chain[target]] = [chain[target], chain[index]];
    const next = JSON.stringify({ default: { providers: chain } }, null, 2);
    setPolicyText(next);
    void savePolicy(next);
  }

  return (
    <div className="space-y-5">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div><span className="workspace-kicker">PROVIDER HEALTH</span><h1 className="mt-1 text-[28px] font-semibold tracking-tight">查询源</h1><p className="text-[12px] text-ink-muted">异常优先展示；链路顺序可调整，默认策略保持 Pi → RDAP → AI。</p></div>
        <button type="button" className="btn min-h-10" onClick={reload}>刷新</button>
      </header>
      {error && <ErrorNotice message={error} onRetry={reload} />}
      {loading && !data && <div className="flex items-center gap-2 py-12 text-ink-muted"><Spinner /> 加载中…</div>}
      {data && <>
        <Card title="查询链路" action={<Pill>拖动替代：使用箭头安全调整</Pill>}>
          <ol className="flex min-w-0 gap-2 overflow-x-auto pb-2">
            {DEFAULT_CHAIN.map((name, index) => {
              const health = byName.get(name);
              return <li key={name} className="flex shrink-0 items-center gap-2">
                <div className={cx("min-w-[150px] rounded-card border border-l-4 bg-surface px-3 py-2", STATE_EDGE[health?.state ?? "unknown"])}>
                  <div className="flex items-center gap-2"><span className={cx("h-2 w-2 rounded-full", STATE_CLASS[health?.state ?? "unknown"])} /><strong className="text-[12px]">{providerLabel(name)}</strong></div>
                  <div className="mt-2 flex gap-1"><button type="button" className="btn h-7 w-7 px-0" disabled={index === 0 || saving} onClick={() => moveProvider(index, -1)} aria-label={`${providerLabel(name)}前移`}>←</button><button type="button" className="btn h-7 w-7 px-0" disabled={index === DEFAULT_CHAIN.length - 1 || saving} onClick={() => moveProvider(index, 1)} aria-label={`${providerLabel(name)}后移`}>→</button></div>
                </div>{index < DEFAULT_CHAIN.length - 1 && <span className="text-ink-faint" aria-hidden="true">→</span>}
              </li>;
            })}
          </ol>
        </Card>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {providers.map((provider) => <div key={provider.provider} className={cx("card border-l-4 p-4", STATE_EDGE[provider.state])}>
            <div className="flex items-center gap-2"><span className={cx("h-2 w-2 rounded-full", STATE_CLASS[provider.state])} /><span className="text-[13px] font-medium">{providerLabel(provider.provider)}</span><Pill className="ml-auto">{STATE_LABEL[provider.state]}</Pill></div>
            <ErrorBars provider={provider} />
            <dl className="mt-3 space-y-1 text-[12px] text-ink-muted"><Row label="近 30 分钟请求" value={String(provider.requests)} /><Row label="错误率" value={`${((provider.error_rate ?? 0) * 100).toFixed(1)}%`} /><Row label="平均 / P95" value={`${formatLatency(provider.avg_latency_ms)} / ${formatLatency(provider.p95_latency_ms ?? 0)}`} /><Row label="连续失败" value={String(provider.consecutive_failures ?? 0)} /><Row label="最近成功" value={formatDateTime(provider.last_success)} /></dl>
            {provider.state_reason && <p className="mt-2 text-[11px] text-ink-faint">{provider.state_reason}</p>}
            {provider.last_error && <p className="mt-2 truncate text-[11px] text-danger" title={provider.last_error}>{provider.last_error}</p>}
            <button type="button" className="btn mt-3 min-h-10 w-full text-[12px]" onClick={() => void testProvider(provider.provider)} disabled={testing === provider.provider}>{testing === provider.provider && <Spinner />}测试此源</button>
          </div>)}
        </div>
        <Card title="IANA RDAP Bootstrap"><dl className="space-y-1 text-[13px]"><Row label="数据源" value={data.bootstrap?.source ?? "—"} /><Row label="状态" value={data.bootstrap?.ok ? `已合并 ${data.bootstrap.merged} 条 TLD 映射` : `不可用（${data.bootstrap?.error ?? "未知原因"}），使用内置映射`} /><Row label="最近更新" value={formatDateTime(data.bootstrap?.last_update)} /></dl></Card>
        <Card title="查询策略" action={<div className="flex gap-2"><button type="button" className="btn h-8 px-2 text-[12px]" onClick={() => setPolicyText(POLICY_EXAMPLE)}>填入示例</button><button type="button" className="btn btn-primary h-8 px-2 text-[12px]" onClick={() => void savePolicy()} disabled={saving}>{saving && <Spinner />}保存</button></div>}><p className="mb-2 text-[12px] text-ink-muted">AI 只在权威来源无法匹配时提供研究说明，不得推翻可注册结论。</p><textarea className="input h-64 resize-y font-mono text-[12px]" value={currentPolicy} onChange={(event) => setPolicyText(event.target.value)} placeholder="{}" spellCheck={false} /></Card>
      </>}
    </div>
  );
}

function ErrorBars({ provider }: { provider: ProviderHealth }) {
  const rate = Math.max(0, Math.min(1, provider.error_rate ?? 0));
  return <div className="mt-3 flex h-7 items-end gap-1" title={`近 30 分钟错误率 ${(rate * 100).toFixed(1)}%`}>{Array.from({ length: 12 }, (_, index) => <span key={index} className={cx("w-full rounded-t-sm", index < Math.round(rate * 12) ? "bg-danger/70" : "bg-success/25")} style={{ height: `${30 + ((index * 17) % 70)}%` }} />)}</div>;
}

function Row({ label, value }: { label: string; value: string }) { return <div className="flex items-center justify-between gap-3"><dt className="shrink-0 text-ink-muted">{label}</dt><dd className="tabular truncate text-right">{value}</dd></div>; }
