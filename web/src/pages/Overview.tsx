import { Link } from "react-router-dom";
import { api } from "../lib/api";
import type { Overview, OverviewItem, ProviderHealth } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import {
  Card,
  EmptyState,
  ErrorNotice,
  Pill,
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

export function OverviewPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const { data, error, loading, reload } = useAsync<Overview>(
    () => api.get<Overview>("/api/v2/overview"),
    [],
    onUnauthorized,
  );

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
            <span className={cx("ml-1", monitorRunning ? "text-emerald-600" : "text-amber-600")}>
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
