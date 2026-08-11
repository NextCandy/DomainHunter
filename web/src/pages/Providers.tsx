import { useState } from "react";
import { api } from "../lib/api";
import type { ProviderHealth } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, ErrorNotice, Pill, Spinner, cx, useToast } from "../components/ui";
import { formatDateTime, formatLatency, providerLabel } from "../lib/format";

interface ProvidersResponse {
  providers: ProviderHealth[] | null;
  policy: unknown;
  bootstrap: {
    attempted: boolean;
    ok: boolean;
    merged: number;
    error?: string;
    last_update?: string;
    source?: string;
  };
}

const STATE_LABEL: Record<ProviderHealth["state"], string> = {
  healthy: "正常",
  degraded: "降级",
  offline: "离线",
  unknown: "暂无数据",
};

const STATE_CLASS: Record<ProviderHealth["state"], string> = {
  healthy: "bg-emerald-500",
  degraded: "bg-amber-500",
  offline: "bg-red-500",
  unknown: "bg-zinc-400",
};

const POLICY_EXAMPLE = `{
  "providers": {
    "whois_ls": { "enabled": true }
  },
  "tlds": {
    "im": {
      "providers": ["whois_ls", "rdap", "whois"],
      "validate_available": true
    },
    "do": {
      "providers": ["fallback", "rdap", "whois"],
      "validate_available": true
    }
  }
}`;

export function ProvidersPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, loading, reload } = useAsync<ProvidersResponse>(
    () => api.get<ProvidersResponse>("/api/v2/providers"),
    [],
    onUnauthorized,
  );
  const [policyText, setPolicyText] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const toast = useToast();

  const currentPolicy =
    policyText ?? (data?.policy ? JSON.stringify(data.policy, null, 2) : "");

  async function savePolicy() {
    setSaving(true);
    try {
      await api.put("/api/v2/settings/query-policy", { policy: currentPolicy.trim() });
      toast("查询策略已保存并生效", "success");
      setPolicyText(null);
      reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : "保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">查询源</h1>
          <p className="text-[12px] text-ink-muted">
            RDAP 优先、WHOIS 兼容；无法明确确认可注册时一律不报告可注册
          </p>
        </div>
        <button type="button" className="btn h-8" onClick={reload}>
          刷新
        </button>
      </header>

      {error && <ErrorNotice message={error} onRetry={reload} />}
      {loading && !data && (
        <div className="flex items-center gap-2 py-12 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      )}

      {data && (
        <>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {(data.providers ?? []).map((provider) => (
              <div key={provider.provider} className="card p-3">
                <div className="flex items-center gap-2">
                  <span className={cx("h-2 w-2 rounded-full", STATE_CLASS[provider.state])} />
                  <span className="text-[13px] font-medium">{providerLabel(provider.provider)}</span>
                  <Pill className="ml-auto">{STATE_LABEL[provider.state]}</Pill>
                </div>
                <dl className="mt-2 space-y-1 text-[12px] text-ink-muted">
                  <Row label="近 30 分钟请求" value={String(provider.requests)} />
                  <Row label="失败" value={String(provider.errors)} />
                  <Row label="平均耗时" value={formatLatency(provider.avg_latency_ms)} />
                  <Row label="最近成功" value={formatDateTime(provider.last_success)} />
                </dl>
                {provider.last_error && (
                  <p
                    className="mt-2 truncate text-[11px] text-red-600 dark:text-red-400"
                    title={provider.last_error}
                  >
                    {provider.last_error}
                  </p>
                )}
              </div>
            ))}
          </div>

          <Card title="IANA RDAP Bootstrap">
            <dl className="space-y-1 text-[13px]">
              <Row label="数据源" value={data.bootstrap?.source ?? "—"} />
              <Row
                label="状态"
                value={
                  data.bootstrap?.ok
                    ? `已合并 ${data.bootstrap.merged} 条 TLD 映射`
                    : `不可用（${data.bootstrap?.error ?? "未知原因"}），正在使用内置静态映射`
                }
              />
              <Row label="最近更新" value={formatDateTime(data.bootstrap?.last_update)} />
            </dl>
          </Card>

          <Card
            title="查询策略"
            action={
              <div className="flex gap-2">
                <button
                  type="button"
                  className="btn h-7 px-2 text-[12px]"
                  onClick={() => setPolicyText(POLICY_EXAMPLE)}
                >
                  填入示例
                </button>
                <button
                  type="button"
                  className="btn btn-primary h-7 px-2 text-[12px]"
                  onClick={savePolicy}
                  disabled={saving}
                >
                  {saving && <Spinner />}保存
                </button>
              </div>
            }
          >
            <p className="mb-2 text-[12px] text-ink-muted">
              留空表示使用默认策略：先查按后缀显式启用的专用源（WHOIS.LS / 备用服务），
              再查 RDAP 与注册局 WHOIS；一旦该后缀配置了专用源，通用源报告的"可注册"
              不会被单独采信。<code className="mono">validate_available</code> 可取
              <code className="mono"> true</code>（等价 distrust）、
              <code className="mono">"confirm"</code>（需要第二个来源印证）或
              <code className="mono">"trust"</code>。
            </p>
            <textarea
              className="input h-64 resize-y font-mono text-[12px]"
              value={currentPolicy}
              onChange={(event) => setPolicyText(event.target.value)}
              placeholder="{}"
              spellCheck={false}
            />
          </Card>
        </>
      )}
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <dt className="shrink-0 text-ink-muted">{label}</dt>
      <dd className="tabular truncate text-right">{value}</dd>
    </div>
  );
}
