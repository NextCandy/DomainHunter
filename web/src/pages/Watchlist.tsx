import { useMemo, useState } from "react";
import { api } from "../lib/api";
import type { DomainInfo, DomainListResult, DomainStatus } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { DomainDrawer } from "../components/DomainDrawer";
import { Card, EmptyState, ErrorNotice, Pill, Spinner, StatusBadge, cx, useToast } from "../components/ui";
import {
  STATUS_LABELS,
  daysUntil,
  formatDate,
  formatRelative,
  providerLabel,
} from "../lib/format";

/**
 * 抢注看板：只看"正在掉落流程里"的域名。
 *
 * 域名列表是全量 + 筛选；这一页刻意只留下需要现在就做决定的那几个，
 * 按抢注紧迫度排序，而不是按域名或到期时间。
 */
const DROP_STATUSES: DomainStatus[] = [
  "available",
  "pending_delete",
  "redemption",
  "expired",
  "grace",
];

// 抢注紧迫度：能注就是最急，其次是马上要删的，宽限期最不急
const URGENCY: Record<string, number> = {
  available: 0,
  pending_delete: 1,
  redemption: 2,
  expired: 3,
  grace: 4,
};

const STATUS_HINT: Record<string, string> = {
  available: "现在就能注册",
  pending_delete: "即将删除，进入抢注窗口",
  redemption: "赎回期，原持有人仍可赎回",
  expired: "已过期，等待进入删除流程",
  grace: "续费宽限期，多数会被续费",
};

export function WatchlistPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [openDomain, setOpenDomain] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const toast = useToast();

  const { data, error, loading, reload } = useAsync<DomainListResult>(
    () =>
      api.get<DomainListResult>(
        `/api/v2/domains?statuses=${DROP_STATUSES.join(",")}&limit=500`,
      ),
    [],
    onUnauthorized,
  );

  const items = useMemo(() => {
    const list = [...(data?.domains ?? [])];
    list.sort((a, b) => {
      const ua = URGENCY[a.status] ?? 99;
      const ub = URGENCY[b.status] ?? 99;
      if (ua !== ub) return ua - ub;
      const da = daysUntil(a.expiry_date);
      const db = daysUntil(b.expiry_date);
      if (da === null && db === null) return a.name.localeCompare(b.name);
      if (da === null) return 1;
      if (db === null) return -1;
      return da - db;
    });
    return list;
  }, [data]);

  const counts = useMemo(() => {
    const map = new Map<string, number>();
    items.forEach((item) => map.set(item.status, (map.get(item.status) ?? 0) + 1));
    return map;
  }, [items]);

  async function runCheck(name: string) {
    setBusy(name);
    try {
      await api.post(`/api/v2/domains/${encodeURIComponent(name)}/check`);
      toast(`${name} 查询完成`, "success");
      reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : "查询失败", "error");
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">抢注看板</h1>
          <p className="text-[12px] text-ink-muted">
            只显示处于掉落流程中的域名，按抢注紧迫度排序。全量清单在「域名」页。
          </p>
        </div>
        <button type="button" className="btn h-8" onClick={reload}>
          刷新
        </button>
      </header>

      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
        {DROP_STATUSES.map((status) => (
          <div key={status} className="card px-3 py-2.5">
            <div className="flex items-center gap-1.5">
              <StatusBadge status={status} />
            </div>
            <div
              className={cx(
                "tabular mt-1 text-[20px] font-semibold leading-tight",
                status === "available" && (counts.get(status) ?? 0) > 0
                  ? "text-emerald-600 dark:text-emerald-400"
                  : "text-ink",
              )}
            >
              {counts.get(status) ?? 0}
            </div>
          </div>
        ))}
      </div>

      {error && <ErrorNotice message={error} onRetry={reload} />}

      {loading && items.length === 0 ? (
        <div className="flex items-center gap-2 py-12 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      ) : items.length === 0 ? (
        <Card>
          <EmptyState
            title="当前没有域名处于掉落流程"
            hint="出现可注册、待删除、赎回期、宽限期或已过期的域名时会自动出现在这里"
          />
        </Card>
      ) : (
        <>
          <div className="hidden md:block">
            <div className="table-scroll card">
              <table className="w-full min-w-[860px] text-left">
                <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
                  <tr>
                    <th className="px-3 py-2 font-medium">域名</th>
                    <th className="px-3 py-2 font-medium">状态</th>
                    <th className="px-3 py-2 font-medium">说明</th>
                    <th className="px-3 py-2 font-medium">到期</th>
                    <th className="px-3 py-2 font-medium">距今</th>
                    <th className="px-3 py-2 font-medium">查询来源</th>
                    <th className="px-3 py-2 font-medium">最后查询</th>
                    <th className="px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line text-[13px]">
                  {items.map((item) => (
                    <Row
                      key={item.name}
                      item={item}
                      busy={busy === item.name}
                      onOpen={() => setOpenDomain(item.name)}
                      onCheck={() => runCheck(item.name)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <ul className="space-y-2 md:hidden">
            {items.map((item) => (
              <MobileCard
                key={item.name}
                item={item}
                busy={busy === item.name}
                onOpen={() => setOpenDomain(item.name)}
                onCheck={() => runCheck(item.name)}
              />
            ))}
          </ul>
        </>
      )}

      <DomainDrawer
        domain={openDomain}
        onClose={() => setOpenDomain(null)}
        onChanged={reload}
        onUnauthorized={onUnauthorized}
      />
    </div>
  );
}

function DaysCell({ item }: { item: DomainInfo }) {
  const days = daysUntil(item.expiry_date);
  if (days === null) return <span className="text-ink-faint">—</span>;
  const overdue = days < 0;
  return (
    <span
      className={cx(
        "tabular",
        overdue
          ? "text-rose-600 dark:text-rose-400"
          : days <= 14
            ? "text-amber-600 dark:text-amber-400"
            : "text-ink-muted",
      )}
    >
      {overdue ? `已过期 ${-days} 天` : `${days} 天后`}
    </span>
  );
}

function Row({
  item,
  busy,
  onOpen,
  onCheck,
}: {
  item: DomainInfo;
  busy: boolean;
  onOpen: () => void;
  onCheck: () => void;
}) {
  return (
    <tr className="hover:bg-surface-muted/60">
      <td className="px-3 py-2">
        <button
          type="button"
          className="mono text-left text-ink hover:text-accent hover:underline"
          onClick={onOpen}
        >
          {item.name}
        </button>
      </td>
      <td className="px-3 py-2">
        <StatusBadge status={item.status} />
      </td>
      <td className="px-3 py-2 text-[12px] text-ink-muted">{STATUS_HINT[item.status] ?? "—"}</td>
      <td className="tabular whitespace-nowrap px-3 py-2 text-ink-muted">
        {formatDate(item.expiry_date)}
      </td>
      <td className="whitespace-nowrap px-3 py-2">
        <DaysCell item={item} />
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
        {providerLabel(item.query_method)}
      </td>
      <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">
        {formatRelative(item.last_checked)}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-right">
        <button
          type="button"
          className="btn btn-ghost h-7 px-2 text-[12px]"
          onClick={onCheck}
          disabled={busy}
        >
          {busy ? "查询中…" : "立即检查"}
        </button>
      </td>
    </tr>
  );
}

function MobileCard({
  item,
  busy,
  onOpen,
  onCheck,
}: {
  item: DomainInfo;
  busy: boolean;
  onOpen: () => void;
  onCheck: () => void;
}) {
  return (
    <li className="card p-3">
      <button type="button" className="mono block w-full truncate text-left text-[13px]" onClick={onOpen}>
        {item.name}
      </button>
      <div className="mt-1.5 flex flex-wrap items-center gap-2">
        <StatusBadge status={item.status} />
        <Pill>{STATUS_HINT[item.status] ?? STATUS_LABELS[item.status]}</Pill>
      </div>
      <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-[12px] text-ink-faint">
        <div className="flex gap-1">
          <dt>到期</dt>
          <dd className="tabular text-ink-muted">{formatDate(item.expiry_date)}</dd>
        </div>
        <div className="flex gap-1">
          <dt>距今</dt>
          <dd>
            <DaysCell item={item} />
          </dd>
        </div>
        <div className="flex gap-1">
          <dt>来源</dt>
          <dd className="truncate text-ink-muted">{providerLabel(item.query_method)}</dd>
        </div>
        <div className="flex gap-1">
          <dt>最后查询</dt>
          <dd className="tabular text-ink-muted">{formatRelative(item.last_checked)}</dd>
        </div>
      </dl>
      <button type="button" className="btn mt-2 h-7 w-full text-[12px]" onClick={onCheck} disabled={busy}>
        {busy ? "查询中…" : "立即检查"}
      </button>
    </li>
  );
}
