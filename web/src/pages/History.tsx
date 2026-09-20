import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "../lib/api";
import type { Attempt, DomainListResult, Observation } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { SearchableMultiSelect } from "../components/SearchableMultiSelect";
import type { SelectOption } from "../components/SearchableMultiSelect";
import { Card, EmptyState, ErrorNotice, FromToStatus, Pill, Skeleton, Spinner, StatusBadge } from "../components/ui";
import { CONFIDENCE_LABELS, formatDateTime, formatLatency, providerLabel } from "../lib/format";

export function HistoryPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [params, setParams] = useSearchParams();
  const domain = (params.get("domain") ?? "").trim().toLowerCase();

  const { data, error, loading, reload } = useAsync<{ observations: Observation[] | null }>(
    () => api.get<{ observations: Observation[] | null }>("/api/v2/observations?limit=200"),
    [],
    onUnauthorized,
  );

  // 域名选择器的选项。原来是一个必须手打完整域名的文本框，
  // 500+ 个域名时基本靠记忆；拉一次全量清单换成可搜索选择。
  const domainList = useAsync<DomainListResult>(
    () => api.get<DomainListResult>("/api/v2/domains?limit=2000"),
    [],
    onUnauthorized,
  );
  const domainOptions = useMemo<SelectOption[]>(
    () => (domainList.data?.domains ?? []).map((item) => ({ value: item.name, label: item.name })),
    [domainList.data],
  );

  const pickDomain = useCallback(
    (name: string) => {
      const next = new URLSearchParams(params);
      if (name) next.set("domain", name);
      else next.delete("domain");
      setParams(next, { replace: true });
    },
    [params, setParams],
  );

  return (
    <div className="space-y-4">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">QUERY HISTORY</span>
          <h1 className="mt-1">查询历史</h1>
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
          <SearchableMultiSelect
            className="w-[min(260px,60vw)]"
            label={domainList.loading ? "载入域名清单…" : `选择域名（${domainOptions.length}）`}
            searchPlaceholder="输入域名以筛选…"
            options={domainOptions}
            selected={domain ? [domain] : []}
            onChange={(next) => pickDomain(next[0] ?? "")}
            multiple={false}
            disabled={domainList.loading}
          />
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
          <EmptyState title="全局还没有任何状态变化" hint="任何域名的状态发生变化后都会自动记录在这里；想看单个域名的完整历史请用上方的域名选择器" />
        ) : (
          <ObservationTable observations={data.observations} onPick={pickDomain} />
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
                {/* 变化记录只显示新状态等于没说发生了什么。 */}
                <FromToStatus from={item.old_status} to={item.status} />
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

  // 同一次观测里若多个源给出不同结论，就是"多源冲突"——这正是这一页要回答的问题，
  // 不能让人自己逐行比对。
  const conflicting = new Set<number>();
  const byObservation = new Map<number, Set<string>>();
  for (const attempt of attempts) {
    if (!attempt.success) continue;
    const set = byObservation.get(attempt.observation_id) ?? new Set<string>();
    set.add(attempt.status);
    byObservation.set(attempt.observation_id, set);
  }
  byObservation.forEach((statuses, id) => {
    if (statuses.size > 1) conflicting.add(id);
  });

  return (
    <div className="grid gap-px bg-line lg:grid-cols-2">
      <section className="bg-surface p-4">
        <h3 className="mb-2 flex items-center gap-2 text-[12px] font-medium text-ink-muted">
          状态时间线 <Pill>{history.length} 条</Pill>
        </h3>
        {history.length === 0 ? (
          <EmptyState title="这个域名还没查过" hint="它还没有产生任何观测记录；执行一次检查后这里就会有状态流" />
        ) : (
          <ObservationTable observations={history} />
        )}
      </section>

      <section className="bg-surface p-4">
        <h3 className="mb-2 flex flex-wrap items-center gap-2 text-[12px] font-medium text-ink-muted">
          查询源历史 <Pill>{attempts.length} 条</Pill>
          {conflicting.size > 0 && (
            <Pill className="border-warn/45 text-warn">{conflicting.size} 次观测存在多源冲突</Pill>
          )}
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
                  <tr
                    key={attempt.id}
                    className={conflicting.has(attempt.observation_id) ? "bg-warn/8" : undefined}
                    title={conflicting.has(attempt.observation_id) ? "这次观测里各查询源结论不一致" : undefined}
                  >
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
