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
  Sparkline,
  Spinner,
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
  healthy: "bg-emerald-500",
  degraded: "bg-amber-500",
  offline: "bg-red-500",
  unknown: "bg-zinc-400",
};

const TREND_COLORS = {
  total: "rgb(var(--accent))",
  available: "rgb(16 185 129)",
  highScore: "rgb(245 158 11)",
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
      <div className="flex items-center gap-2 py-16 text-ink-muted">
        <Spinner /> 正在加载概览…
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
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">概览</h1>
          <p className="text-[12px] text-ink-muted">
            共 {data.total} 个域名 · 调度器
            <span className={cx("ml-1", monitorRunning ? "text-emerald-700 dark:text-emerald-300" : "text-amber-700 dark:text-amber-300")}>
              {monitorRunning ? "运行中" : "已停止"}
            </span>
            （{workers} worker，队列 {queued}）
          </p>
        </div>
        <button type="button" className="btn h-8" onClick={reload}>
          刷新
        </button>
      </header>

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
        <StatTile label="总域名" value={data.total} tone="ink" />
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
          />
        ))}
      </div>

      <TrendCard points={trendPoints} usingFallback={usingFallback} />

      <div className="grid gap-4 xl:grid-cols-2">
        <Card title="最近状态变化" bodyClassName="p-0">
          <ItemList
            items={data.recent_changes}
            empty="暂无状态变化"
            render={(item) => (
              <>
                <StatusBadge status={item.status} />
                <span className="text-[12px] text-ink-faint">{formatRelative(item.observed_at)}</span>
              </>
            )}
          />
        </Card>

        <Card title="即将到期（60 天内）" bodyClassName="p-0">
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
                      days !== null && days <= 7 ? "text-red-600 dark:text-red-400" : "text-ink-faint",
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
      <Sparkline series={series} />
      <div className="mt-2 flex justify-between text-[11px] text-ink-faint">
        <span>{formatTrendDay(points[0]?.day)}</span>
        <span>{formatTrendDay(points[points.length - 1]?.day)}</span>
      </div>
    </Card>
  );
}

function normalizeTrend(payload: unknown): OverviewTrendPoint[] {
  const points = extractTrendPoints(payload);
  return points
    .map((point, index) => {
      if (!point || typeof point !== "object") return null;
      const value = point as Record<string, unknown>;
      const day = normalizeDay(value.day ?? value.date ?? value.label ?? value.timestamp) ?? `day-${index}`;
      return {
        day,
        total: readNumber(value.total, value.count, value.domains, value.new_domains, value.added),
        available: readNumber(value.available, value.available_count, value.free),
        high_score: readNumber(value.high_score, value.highScore, value.high, value.score_80_plus),
        changes: readNumber(value.changes, value.status_changes, value.change_count),
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
  const points = Array.from({ length: 7 }, (_, index) => {
    const date = new Date(today);
    date.setDate(today.getDate() - 6 + index);
    return { day: localDayKey(date), total: 0, available: 0, high_score: 0, changes: 0 };
  });
  const byDay = new Map(points.map((point) => [point.day, point]));

  for (const item of data.recent_changes ?? []) {
    const day = normalizeDay(item.observed_at);
    const point = day ? byDay.get(day) : undefined;
    if (point) {
      point.changes += 1;
      point.total += 1;
    }
  }
  for (const item of data.recent_available ?? []) {
    const day = normalizeDay(item.observed_at);
    const point = day ? byDay.get(day) : undefined;
    if (point) point.available += 1;
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
}: {
  label: string;
  value: number;
  tone: "ink" | "accent";
}) {
  return (
    <div className="card px-3 py-2.5">
      <div className="text-[11px] text-ink-muted">{label}</div>
      <div
        className={cx(
          "tabular mt-0.5 text-[20px] font-semibold leading-tight",
          tone === "accent" && value > 0 ? "text-emerald-600 dark:text-emerald-400" : "text-ink",
        )}
      >
        {value}
      </div>
    </div>
  );
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
            to={`/domains?search=${encodeURIComponent(item.domain)}`}
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
