import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "../lib/api";
import type { Attempt, Observation } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { Card, EmptyState, ErrorNotice, Pill, Skeleton, Spinner, StatusBadge } from "../components/ui";
import { CONFIDENCE_LABELS, formatDateTime, formatLatency, providerLabel } from "../lib/format";

export function HistoryPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [params, setParams] = useSearchParams();
  const domain = (params.get("domain") ?? "").trim().toLowerCase();
  const [input, setInput] = useState(domain);

  useEffect(() => setInput(domain), [domain]);

  const { data, error, loading, reload } = useAsync<{ observations: Observation[] | null }>(
    () => api.get<{ observations: Observation[] | null }>("/api/v2/observations?limit=200"),
    [],
    onUnauthorized,
  );

  return (
    <div className="space-y-4">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">QUERY HISTORY</span>
          <h1 className="mt-1 text-[28px] font-semibold tracking-tight">查询历史</h1>
          <p className="text-[12px] text-ink-muted">
            状态流转记录与每个查询源的历史结论，用于回答"当前状态为什么是这个结果"
          </p>
        </div>
        <button type="button" className="btn h-8" onClick={reload}>
          刷新
        </button>
      </header>

      <Card
        title="按域名查看"
        action={
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              const next = new URLSearchParams(params);
              const name = input.trim().toLowerCase();
              if (name) next.set("domain", name); else next.delete("domain");
              setParams(next, { replace: true });
            }}
          >
            <input
              className="input h-7 w-[200px] py-0 text-[12px]"
              placeholder="example.com"
              value={input}
              onChange={(event) => setInput(event.target.value)}
            />
            <button type="submit" className="btn h-7 px-2 text-[12px]">
              查看
            </button>
          </form>
        }
        bodyClassName="p-0"
      >
        {domain ? (
          <DomainHistory domain={domain} onUnauthorized={onUnauthorized} />
        ) : (
          <EmptyState title="输入域名查看它的完整状态时间线与查询源历史" />
        )}
      </Card>

      <Card title="全局最近状态变化" bodyClassName="p-0">
        {error && (
          <div className="p-4">
            <ErrorNotice message={error} onRetry={reload} />
          </div>
        )}
      {loading && !data ? (
          <div className="space-y-2 p-4" aria-label="查询历史加载中">
            {Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="h-9 w-full" />)}
          </div>
        ) : !data?.observations || data.observations.length === 0 ? (
          <EmptyState title="暂无状态变化记录" hint="域名状态发生变化后会自动记录在这里" />
        ) : (
          <ObservationTable
            observations={data.observations}
            onPick={(name) => {
              setInput(name);
              const next = new URLSearchParams(params);
              next.set("domain", name);
              setParams(next, { replace: true });
            }}
          />
        )}
      </Card>
    </div>
  );
}

function ObservationTable({
  observations,
  onPick,
}: {
  observations: Observation[];
  onPick?: (name: string) => void;
}) {
  return (
    <div className="table-scroll">
      <table className="w-full min-w-[640px] text-left">
        <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
          <tr>
            <th className="px-4 py-2 font-medium">时间</th>
            <th className="px-4 py-2 font-medium">域名</th>
            <th className="px-4 py-2 font-medium">状态</th>
            <th className="px-4 py-2 font-medium">查询源</th>
            <th className="px-4 py-2 font-medium">可信度</th>
            <th className="px-4 py-2 font-medium">注册商</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line text-[13px]">
          {observations.map((item) => (
            <tr key={item.id} className="hover:bg-surface-muted/60">
              <td className="tabular whitespace-nowrap px-4 py-2 text-ink-muted">
                {formatDateTime(item.observed_at)}
              </td>
              <td className="px-4 py-2">
                {onPick ? (
                  <button
                    type="button"
                    className="mono text-ink hover:text-accent hover:underline"
                    onClick={() => onPick(item.domain)}
                  >
                    {item.domain}
                  </button>
                ) : (
                  <span className="mono">{item.domain}</span>
                )}
              </td>
              <td className="px-4 py-2">
                <StatusBadge status={item.status} />
              </td>
              <td className="whitespace-nowrap px-4 py-2 text-ink-muted">
                {providerLabel(item.provider)}
              </td>
              <td className="whitespace-nowrap px-4 py-2 text-ink-muted">
                {CONFIDENCE_LABELS[item.confidence] ?? item.confidence ?? "—"}
              </td>
              <td className="max-w-[200px] truncate px-4 py-2 text-ink-muted" title={item.registrar}>
                {item.registrar || "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function DomainHistory({
  domain,
  onUnauthorized,
}: {
  domain: string;
  onUnauthorized: () => void;
}) {
  const [history, setHistory] = useState<Observation[]>([]);
  const [attempts, setAttempts] = useState<Attempt[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [historyResult, attemptsResult] = await Promise.all([
        api.get<{ history: Observation[] | null }>(
          `/api/domains/${encodeURIComponent(domain)}/history?limit=200`,
        ),
        api.get<{ attempts: Attempt[] | null }>(
          `/api/domains/${encodeURIComponent(domain)}/attempts?limit=100`,
        ),
      ]);
      setHistory(historyResult.history ?? []);
      setAttempts(attemptsResult.attempts ?? []);
    } catch (err) {
      if (err instanceof Error && err.message.includes("会话")) onUnauthorized();
      setError(err instanceof Error ? err.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, [domain, onUnauthorized]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 p-4 text-ink-muted">
        <Spinner /> 加载中…
      </div>
    );
  }
  if (error) {
    return (
      <div className="p-4">
        <ErrorNotice message={error} onRetry={load} />
      </div>
    );
  }

  return (
    <div className="divide-y divide-line">
      <section className="p-4">
        <h3 className="mb-2 flex items-center gap-2 text-[12px] font-medium text-ink-muted">
          状态时间线 <Pill>{history.length} 条</Pill>
        </h3>
        {history.length === 0 ? (
          <EmptyState title="该域名还没有观测记录" />
        ) : (
          <ObservationTable observations={history} />
        )}
      </section>

      <section className="p-4">
        <h3 className="mb-2 flex items-center gap-2 text-[12px] font-medium text-ink-muted">
          查询源历史 <Pill>{attempts.length} 条</Pill>
        </h3>
        {attempts.length === 0 ? (
          <EmptyState title="该域名还没有查询尝试记录" />
        ) : (
          <div className="table-scroll">
            <table className="w-full min-w-[560px] text-left">
              <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
                <tr>
                  <th className="px-4 py-2 font-medium">时间</th>
                  <th className="px-4 py-2 font-medium">查询源</th>
                  <th className="px-4 py-2 font-medium">结果</th>
                  <th className="px-4 py-2 text-right font-medium">耗时</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line text-[13px]">
                {attempts.map((attempt) => (
                  <tr key={attempt.id}>
                    <td className="tabular whitespace-nowrap px-4 py-2 text-ink-muted">
                      {formatDateTime(attempt.queried_at)}
                    </td>
                    <td className="whitespace-nowrap px-4 py-2">{providerLabel(attempt.provider)}</td>
                    <td className="px-4 py-2">
                      {attempt.success ? (
                        <StatusBadge status={attempt.status} />
                      ) : (
                        <span
                          className="text-[12px] text-ink-muted"
                          title={attempt.error_message}
                        >
                          {attempt.error_message || "失败"}
                        </span>
                      )}
                    </td>
                    <td className="tabular whitespace-nowrap px-4 py-2 text-right text-ink-faint">
                      {formatLatency(attempt.latency_ms)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
