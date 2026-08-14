import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Overview, OverviewItem, OverviewTrendPoint, ProviderHealth } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import {
  Card,
  EmptyState,
  ErrorNotice,
  Pill,
  Skeleton,
  Sparkline,
  StatusBadge,
  cx,
} from "../components/ui";
import {
  STATUS_LABELS,
  STATUS_ORDER,
  daysUntil,
  formatDate,
  formatLatency,
  formatRelative,
  providerLabel,
} from "../lib/format";

const HEALTH_LABELS: Record<ProviderHealth["state"], string> = {
  healthy: "正常",
  degraded: "降级",
  offline: "离线",
  unknown: "暂无数据",
};

const HEALTH_DOT: Record<ProviderHealth["state"], string> = {
  healthy: "bg-success",
  degraded: "bg-warning",
  offline: "bg-danger",
  unknown: "bg-neutral",
};

const TREND_COLORS = {
  total: "oklch(var(--info))",
  available: "oklch(var(--success))",
  highScore: "oklch(var(--warning))",
};

export function OverviewPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, loading, reload } = useAsync<Overview>(
    () => api.get<Overview>("/api/v2/overview"),
    [],
    onUnauthorized,
  );
  const [trend, setTrend] = useState<OverviewTrendPoint[] | null>(null);

  useEffect(() => {
    let active = true;
    void api
      .getOptional<unknown>("/api/v2/overview/trend?days=7")
      .then((payload) => {
        if (active) setTrend(normalizeTrend(payload));
      })
      .catch((err) => {
        if (err instanceof Error && err.message.includes("会话")) onUnauthorized();
        if (active) setTrend([]);
      });

    return () => {
      active = false;
    };
  }, [onUnauthorized]);

  const fallbackTrend = useMemo(() => (data ? buildFallbackTrend(data) : []), [data]);
  const trendPoints = trend && trend.length > 0 ? trend : fallbackTrend;
  const usingFallback = !trend || trend.length === 0;

  if (loading && !data) {
    return (
      <div className="space-y-6 py-2" aria-label="概览加载中">
        <div className="space-y-3"><Skeleton className="h-3 w-32" /><Skeleton className="h-12 w-64" /><Skeleton className="h-4 w-80 max-w-full" /></div>
        <div className="grid grid-cols-2 gap-px overflow-hidden rounded-card border border-line bg-line md:grid-cols-4">{Array.from({ length: 8 }, (_, index) => <Skeleton key={index} className="h-28 rounded-none bg-surface" />)}</div>
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 4 }, (_, index) => <Skeleton key={index} className="h-32 rounded-card" />)}</div>
        <div className="card p-6"><Skeleton className="h-5 w-40" /><Skeleton className="mt-5 h-32 w-full" /></div>
      </div>
    );
  }
  if (error) return <ErrorNotice message={error} onRetry={reload} />;
  if (!data) return null;

  const monitorRunning = Boolean(data.monitor?.["is_running"]);
  const workers = Number(data.monitor?.["worker_count"] ?? 0);
  const queued =
    Number(data.monitor?.["queue_manual"] ?? 0) +
    Number(data.monitor?.["queue_retry"] ?? 0) +
    Number(data.monitor?.["queue_scheduled"] ?? 0);

  return (
    <div className="space-y-8">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">DOMAIN WORKSPACE</span>
          <h1 className="editorial-title mt-2 text-[38px] leading-tight sm:text-[52px]">今日工作台</h1>
          <p className="text-[12px] text-ink-muted">
            共 {data.total} 个域名 · 调度器
            <span className={cx("ml-1", monitorRunning ? "text-accent" : "text-ink-muted")}>
              {monitorRunning ? "运行中" : "已停止"}
            </span>
            （{workers} worker，队列 {queued}）
          </p>
        </div>
        <button type="button" className="btn btn-secondary" onClick={reload}>
          刷新
        </button>
      </header>

      <div className="overview-stats-band">
        <div className="relative grid grid-cols-2 gap-0 md:grid-cols-4">
          <StatTile label="总域名" value={data.total} tone="ink" sparkline={trendPoints.map((point) => point.total)} sparkColor={TREND_COLORS.total} />
          {STATUS_ORDER.filter((status) =>
            ["available", "registered", "grace", "redemption", "pending_delete", "error", "skipped"].includes(
              status,
            ),
          ).map((status) => (
            <StatTile
              key={status}
              label={STATUS_LABELS[status]}
              value={data.status_counts?.[status] ?? 0}
              tone={status === "available" ? "accent" : "ink"}
              sparkline={trendPoints.map((point) => status === "available" ? point.available : point.status_counts?.[status] ?? 0)}
              sparkColor={status === "available" ? TREND_COLORS.available : TREND_COLORS.total}
            />
          ))}
        </div>
      </div>

      <ActionQueue
        counts={data.action_counts}
      />

      <ProviderAlert providers={data.providers} />

      <TrendCard points={trendPoints} usingFallback={usingFallback} />

      <div className="grid gap-4 xl:grid-cols-2">
        <Card title="最近状态变化" bodyClassName="p-0">
          <ItemList
            items={data.recent_changes}
            empty="暂无状态变化"
            render={(item) => (
              <>
                <StatusBadge status={item.status} />
                {item.message && <Pill>{item.message}</Pill>}
                <span className="text-[12px] text-ink-faint">{formatRelative(item.observed_at)}</span>
              </>
            )}
          />
        </Card>

      <Card title="即将到期（60 天内）" bodyClassName="p-0">
          <RenewalSummary items={data.upcoming_expiry} />
          <ItemList
            items={data.upcoming_expiry}
            empty="60 天内没有到期的域名"
            render={(item) => {
              const days = daysUntil(item.expiry_at);
              return (
                <>
                  <span className="tabular text-[12px] text-ink-muted">{formatDate(item.expiry_at)}</span>
                  <span
                    className={cx(
                      "tabular text-[12px]",
                      days !== null && days <= 7 ? "text-ink" : "text-ink-faint",
                    )}
                  >
                    {days !== null ? `${days} 天` : "—"}
                  </span>
                </>
              );
            }}
          />
        </Card>

        <Card title="最近可注册" bodyClassName="p-0">
          <ItemList
            items={data.recent_available}
            empty="当前没有可注册的域名"
            render={(item) => (
              <>
                <Pill>{providerLabel(item.provider)}</Pill>
                <span className="text-[12px] text-ink-faint">{formatRelative(item.observed_at)}</span>
              </>
            )}
          />
        </Card>

        <Card title="查询失败" bodyClassName="p-0">
          <ItemList
            items={data.query_failures}
            empty="没有查询失败的域名"
            render={(item) => (
              <span className="max-w-[220px] truncate text-[12px] text-ink-faint" title={item.message}>
                {item.message || "—"}
              </span>
            )}
          />
        </Card>
      </div>

      <Card
        title="查询源健康状态"
        action={
          <Link to="/providers" className="text-[12px] text-accent hover:underline">
            查看详情
          </Link>
        }
        bodyClassName="p-0"
      >
        {!data.providers || data.providers.length === 0 ? (
          <EmptyState title="暂无查询源数据" />
        ) : (
          <ul className="divide-y divide-line">
            {data.providers.map((provider) => (
              <li
                key={provider.provider}
                className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2.5"
              >
                <span className={cx("h-2 w-2 shrink-0 rounded-full", HEALTH_DOT[provider.state])} />
                <span className="w-[92px] text-[13px] font-medium">
                  {providerLabel(provider.provider)}
                </span>
                <span className="w-[64px] text-[12px] text-ink-muted">
                  {HEALTH_LABELS[provider.state]}
                </span>
                <span className="tabular text-[12px] text-ink-faint">
                  {provider.errors} 错误 / {provider.requests} 请求
                  {provider.error_rate != null && ` · ${(provider.error_rate * 100).toFixed(1)}%`}
                </span>
                <span className="tabular ml-auto text-[12px] text-ink-faint">
                  平均 {formatLatency(provider.avg_latency_ms)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function RenewalSummary({ items }: { items: OverviewItem[] | null }) {
  const thresholds = [60, 30, 7, 1];
  const counts = thresholds.map((threshold) => (items ?? []).filter((item) => { const days = daysUntil(item.expiry_at); return days !== null && days >= 0 && days <= threshold; }).length);
  const registrarGroups = new Map<string, number>();
  for (const item of items ?? []) registrarGroups.set(item.registrar || "未识别注册商", (registrarGroups.get(item.registrar || "未识别注册商") ?? 0) + 1);
  return <div className="border-b border-line bg-surface-muted/45 px-4 py-3"><div className="grid grid-cols-4 gap-2">{thresholds.map((threshold, index) => <div key={threshold} className="text-center"><strong className={cx("tabular block text-[18px]", threshold <= 7 ? "text-danger" : threshold <= 30 ? "text-warning" : "text-neutral")}>{counts[index]}</strong><span className="text-[10px] text-ink-faint">{threshold} 天内</span></div>)}</div>{registrarGroups.size > 0 && <p className="mt-2 truncate text-[10px] text-ink-muted" title={Array.from(registrarGroups).map(([name, count]) => `${name} ${count}`).join("、")}>按注册商：{Array.from(registrarGroups).slice(0, 4).map(([name, count]) => `${name} ${count}`).join(" · ")}</p>}</div>;
}

function TrendCard({
  points,
  usingFallback,
}: {
  points: OverviewTrendPoint[];
  usingFallback: boolean;
}) {
  const series = usingFallback
    ? [
        {
          label: "状态变化",
          values: points.map((point) => point.changes),
          color: TREND_COLORS.total,
        },
        {
          label: "可注册变化",
          values: points.map((point) => point.available),
          color: TREND_COLORS.available,
        },
      ]
    : [
        {
          label: "新增域名",
          values: points.map((point) => point.total),
          color: TREND_COLORS.total,
        },
        {
          label: "可注册",
          values: points.map((point) => point.available),
          color: TREND_COLORS.available,
        },
        {
          label: "高分域名",
          values: points.map((point) => point.high_score),
          color: TREND_COLORS.highScore,
        },
      ];

  return (
    <Card
      title="最近 7 天趋势"
      action={
        <Pill title={usingFallback ? "趋势接口不可用，使用现有概览摘要" : "来自趋势接口"}>
          {usingFallback ? "摘要降级" : "实时数据"}
        </Pill>
      }
    >
      {series.every((item) => item.values.every((value) => value === 0)) ? (
        <EmptyState title="近 7 天无状态变化" hint="下一次状态改变后会在这里形成趋势" />
      ) : (
        <Sparkline series={series} labels={points.map((point) => formatTrendDay(point.day))} />
      )}
      <div className="mt-2 flex justify-between text-[11px] text-ink-faint">
        <span>{formatTrendDay(points[0]?.day)}</span>
        <span>{formatTrendDay(points[points.length - 1]?.day)}</span>
      </div>
    </Card>
  );
}

function ActionQueue({ counts }: { counts: Overview["action_counts"] }) {
  const items = [
    { label: "立即行动", count: counts.available, hint: "高可信可注册结果", href: "/domains?status=available", tone: "text-accent" },
    { label: "抢注窗口", count: counts.drop_window, hint: "高可信掉落流程", href: "/watchlist", tone: "text-ink" },
    { label: "续费风险", count: counts.renewal_risk, hint: "未来 7 天内到期", href: "/domains?sort=expiry", tone: "text-ink" },
    { label: "需要复核", count: counts.review, hint: "查询事实或证据异常", href: "/domains?statuses=error,unknown,skipped", tone: "text-review" },
  ];
  const borders = ["border-l-success", "border-l-info", "border-l-warning", "border-l-danger"];
  return <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{items.map((item, index) => <Link key={item.label} to={item.href} className={cx("action-queue-item card block min-h-[134px] border-l-4 px-6 py-5 transition-colors hover:bg-accent-soft/45 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent sm:px-7", borders[index])}><div className="flex items-start justify-between gap-4"><span className={cx("text-[13px] font-semibold", item.tone)}>{item.label}</span><span className={cx("tabular text-[24px] font-semibold leading-none", index === 0 ? "text-success" : index === 1 ? "text-info" : index === 2 ? "text-warning" : "text-danger")}>{item.count}</span></div><p className="mt-4 text-[11px] text-ink-muted">{item.hint}</p><span className="mt-3 block text-[11px] font-medium text-accent">打开工作区 →</span></Link>)}</div>;
}

function ProviderAlert({ providers }: { providers: ProviderHealth[] | null }) {
  const atRisk = (providers ?? []).filter((provider) => provider.state === "offline" || (provider.error_rate ?? 0) > 0.2);
  if (atRisk.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-card border border-danger/35 bg-danger/8 px-4 py-3 text-[12px] text-ink" role="alert">
      <span className="min-w-0 flex-1">
        <strong className="text-danger">查询源需要关注：</strong>{" "}
        {atRisk.map((provider) => `${providerLabel(provider.provider)}（${provider.state === "offline" ? "离线" : `错误率 ${((provider.error_rate ?? 0) * 100).toFixed(1)}%`}）`).join("、")}
      </span>
      <Link to="/providers" className="shrink-0 font-medium text-danger underline underline-offset-2">查看查询源</Link>
    </div>
  );
}

function normalizeTrend(payload: unknown): OverviewTrendPoint[] {
  const points = extractTrendPoints(payload);
  return points
    .map((point, index): OverviewTrendPoint | null => {
      if (!point || typeof point !== "object") return null;
      const value = point as Record<string, unknown>;
      const day = normalizeDay(value.day ?? value.date ?? value.label ?? value.timestamp) ?? `day-${index}`;
      return {
        day,
        total: readNumber(value.total, value.count, value.domains, value.new_domains, value.added),
        available: readNumber(value.available, value.available_count, value.free),
        high_score: readNumber(value.high_score, value.highScore, value.high, value.score_80_plus),
        changes: readNumber(value.changes, value.status_changes, value.change_count),
        status_counts: normalizeStatusCounts(value.status_counts),
      };
    })
    .filter((point): point is OverviewTrendPoint => point !== null)
    .slice(-7);
}

function extractTrendPoints(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  if (!payload || typeof payload !== "object") return [];
  const value = payload as Record<string, unknown>;
  for (const key of ["points", "trend", "data", "items"]) {
    if (Array.isArray(value[key])) return value[key];
  }
  return [];
}

function readNumber(...values: unknown[]): number {
  for (const value of values) {
    const number = typeof value === "number" ? value : Number(value);
    if (Number.isFinite(number)) return Math.max(0, number);
  }
  return 0;
}

function normalizeDay(value: unknown): string | null {
  if (typeof value !== "string" && typeof value !== "number") return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return null;
  return localDayKey(date);
}

function localDayKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function buildFallbackTrend(data: Overview): OverviewTrendPoint[] {
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const points: OverviewTrendPoint[] = Array.from({ length: 7 }, (_, index) => {
    const date = new Date(today);
    date.setDate(today.getDate() - 6 + index);
    return { day: localDayKey(date), total: 0, available: 0, high_score: 0, changes: 0, status_counts: {} };
  });
  const byDay = new Map(points.map((point) => [point.day, point]));

  for (const item of data.recent_changes ?? []) {
    const day = normalizeDay(item.observed_at);
    const point = day ? byDay.get(day) : undefined;
    if (point) {
      const counts = point.status_counts ?? (point.status_counts = {});
      point.changes += 1;
      point.total += 1;
      counts[item.status] = (counts[item.status] ?? 0) + 1;
    }
  }
  for (const item of data.recent_available ?? []) {
    const day = normalizeDay(item.observed_at);
    const point = day ? byDay.get(day) : undefined;
    if (point) {
      const counts = point.status_counts ?? (point.status_counts = {});
      point.available += 1;
      counts.available = (counts.available ?? 0) + 1;
    }
  }
  return points;
}

function formatTrendDay(value?: string): string {
  if (!value || value.startsWith("day-")) return "—";
  const date = new Date(`${value}T00:00:00`);
  if (Number.isNaN(date.getTime())) return "—";
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

function StatTile({
  label,
  value,
  tone,
  sparkline = [],
  sparkColor = TREND_COLORS.total,
}: {
  label: string;
  value: number;
  tone: "ink" | "accent";
  sparkline?: number[];
  sparkColor?: string;
}) {
  const previous = sparkline.length > 1 ? sparkline[sparkline.length - 2] ?? 0 : value;
  const current = sparkline.length > 0 ? sparkline[sparkline.length - 1] ?? value : value;
  const delta = current - previous;
  return (
    <div className="overview-stat-tile min-h-[126px] px-5 py-5 sm:px-6 sm:py-6">
      <div className="font-mono text-[10px] uppercase tracking-[-0.02em] text-ink-muted">{label}</div>
      <div className="mt-2 flex items-end justify-between gap-2">
        <div className={cx("tabular font-display text-[30px] font-normal leading-tight", tone === "accent" && value > 0 ? "text-accent" : "text-ink")}>{value}</div>
        <MiniSparkline values={sparkline} color={sparkColor} label={`${label} 最近七天`} />
      </div>
      <div className={cx("mt-1 text-right text-[10px]", delta > 0 ? "text-success" : delta < 0 ? "text-danger" : "text-ink-faint")}>
        {delta > 0 ? `↑${delta}` : delta < 0 ? `↓${Math.abs(delta)}` : "—"} · 7 天
      </div>
    </div>
  );
}

function MiniSparkline({ values, color, label }: { values: number[]; color: string; label: string }) {
  const points = values.length > 1 ? values : [values[0] ?? 0, values[0] ?? 0];
  const max = Math.max(1, ...points);
  const path = points.map((value, index) => {
    const x = 2 + (index / Math.max(1, points.length - 1)) * 56;
    const y = 18 - (value / max) * 14;
    return `${index === 0 ? "M" : "L"}${x.toFixed(1)} ${y.toFixed(1)}`;
  }).join(" ");
  return <svg className="h-6 w-16 shrink-0 overflow-visible" viewBox="0 0 60 20" role="img" aria-label={label}><path d="M2 18H58" stroke="rgb(var(--line))" strokeWidth="0.8" /><path d={path} fill="none" stroke={color} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>;
}

function normalizeStatusCounts(value: unknown): Record<string, number> {
  if (!value || typeof value !== "object") return {};
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, readNumber(item)]));
}

function ItemList({
  items,
  empty,
  render,
}: {
  items: OverviewItem[] | null;
  empty: string;
  render: (item: OverviewItem) => React.ReactNode;
}) {
  if (!items || items.length === 0) return <EmptyState title={empty} />;
  return (
    <ul className="divide-y divide-line">
      {items.map((item, index) => (
        <li
          key={`${item.domain}-${index}`}
          className="flex items-center justify-between gap-3 px-4 py-2"
        >
          <Link
            to={`/history?domain=${encodeURIComponent(item.domain)}`}
            className="mono truncate text-ink hover:text-accent hover:underline"
          >
            {item.domain}
          </Link>
          <div className="flex shrink-0 items-center gap-2">{render(item)}</div>
        </li>
      ))}
    </ul>
  );
}
