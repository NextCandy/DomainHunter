import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../lib/api";
import type { DomainInfo, DomainListResult, DomainStatus } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { DomainDrawer } from "../components/DomainDrawer";
import {
  Card,
  DomainName,
  EmptyState,
  ErrorNotice,
  Pill,
  ReviewIndicator,
  Skeleton,
  StatusBadge,
  cx,
  useToast,
} from "../components/ui";
import {
  STATUS_LABELS,
  daysUntil,
  formatDate,
  formatExpiryRelative,
  formatRelative,
  providerLabel,
  predictReleaseWindow,
  tldOf,
} from "../lib/format";
import { registrarShortName } from "../lib/registrar";
import { RegistrarLookupMenu } from "../components/RegistrarLookupMenu";

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

const DROP_STAGES: Array<{ status: DomainStatus; hint: string }> = [
  { status: "grace", hint: "等待是否续费" },
  { status: "expired", hint: "已过期" },
  { status: "redemption", hint: "仍可赎回" },
  { status: "pending_delete", hint: "即将释放" },
  { status: "available", hint: "可以注册" },
];

export function WatchlistPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [openDomain, setOpenDomain] = useState<string | null>(null);
  const navigate = useNavigate();
  const [busy, setBusy] = useState<string | null>(null);
  const [inspected, setInspected] = useState<string | null>(null);
  const toast = useToast();

  const { data, error, loading, reload } = useAsync<DomainListResult>(
    () =>
      api.get<DomainListResult>(
        `/api/v2/domains?statuses=${DROP_STATUSES.join(",")}&limit=2000`,
      ),
    [],
    onUnauthorized,
  );

  const items = useMemo(() => {
    const list = [...(data?.domains ?? [])];
    list.sort((a, b) => {
      const reviewOrder = Number(Boolean(a.review?.required)) - Number(Boolean(b.review?.required));
      if (reviewOrder !== 0) return reviewOrder;
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

  // 选中卡片的完整记录；items 刷新后自动跟上，不会指向一条陈旧的快照。
  const inspectedItem = items.find((item) => item.name === inspected) ?? null;
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

  async function setReminder(item: DomainInfo) {
    try {
      const next = item.notify === false;
      await api.patch(`/api/v2/domains/${encodeURIComponent(item.name)}`, { notify: next });
      toast(next ? `${item.name} 已恢复提醒` : `${item.name} 已忽略后续提醒`, "success");
      reload();
    } catch (err) {
      toast(err instanceof Error ? err.message : "设置提醒失败", "error");
    }
  }

  return (
    <div className="space-y-4">
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">DROP WINDOW</span>
          <h1 className="mt-1">抢注看板</h1>
          <p className="text-[12px] text-ink-muted">
            只显示处于掉落流程中的域名，按抢注紧迫度排序。全量清单在「域名」页。
          </p>
        </div>
        <button type="button" className="btn h-8" onClick={reload}>
          刷新
        </button>
      </header>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {DROP_STATUSES.map((status) => (
          <div key={status} className="card min-h-[108px] px-6 py-5 sm:px-7">
            <div className="flex items-center gap-1.5">
              <StatusBadge status={status} />
            </div>
            <div
              className={cx(
                "tabular font-mono mt-3 text-[22px] font-semibold leading-tight",
                status === "available" && (counts.get(status) ?? 0) > 0
                  ? "text-accent"
                  : "text-ink",
              )}
            >
              {counts.get(status) ?? 0}
            </div>
          </div>
        ))}
      </div>

      <DropBoard counts={counts} />

      {error && <ErrorNotice message={error} onRetry={reload} />}

      {loading && items.length === 0 ? (
        <div className="space-y-3" aria-label="抢注看板加载中">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">{Array.from({ length: 5 }, (_, index) => <Skeleton key={index} className="h-24 rounded-card" />)}</div>
          <Skeleton className="h-56 w-full rounded-card" />
          <div className="card space-y-3 p-4">{Array.from({ length: 5 }, (_, index) => <Skeleton key={index} className="h-10 w-full" />)}</div>
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
          {/* 桌面五列看板：掉落流程本身就是五个阶段，横着摆一眼看出
              "哪一段堆着东西"，比一张按紧迫度排序的长表更接近人的判断方式。 */}
          <div className="hidden gap-3 md:grid xl:grid-cols-[minmax(0,1fr)_320px]">
            <div className="grid min-w-0 gap-3 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
              {DROP_STAGES.map(({ status }) => {
                const columnItems = items.filter((item) => item.status === status);
                return (
                  <section key={status} className="min-w-0">
                    <header className="mb-2 flex items-center justify-between gap-2 border-b border-line pb-1.5">
                      <StatusBadge status={status} />
                      <span className="tabular text-[11px] text-ink-faint">{columnItems.length}</span>
                    </header>
                    {columnItems.length === 0 ? (
                      /* 空列只留一行字——空状态插画在五列里会把整块撑得老高。 */
                      <p className="px-1 py-2 text-[11px] text-ink-faint">暂无</p>
                    ) : (
                      <ul className="space-y-2">
                        {columnItems.map((item) => (
                          <li key={item.name}>
                            <KanbanCard
                              item={item}
                              active={inspected === item.name}
                              onSelect={() => setInspected(inspected === item.name ? null : item.name)}
                              onOpen={() => setOpenDomain(item.name)}
                              onRemind={() => void setReminder(item)}
                            />
                          </li>
                        ))}
                      </ul>
                    )}
                  </section>
                );
              })}
            </div>

            {/* 选中一张卡后右侧展开细节：释放预测、注册商入口、提醒开关。
                不选就不占位，看板保持满宽。 */}
            {inspectedItem && (
              <aside className="card hidden h-max p-4 xl:block">
                <div className="flex items-start justify-between gap-2">
                  <DomainName name={inspectedItem.name} onClick={() => setOpenDomain(inspectedItem.name)} className="text-[15px]" />
                  <button type="button" className="btn btn-ghost h-7 w-7 shrink-0 px-0" onClick={() => setInspected(null)} aria-label="关闭详情">×</button>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-2">
                  <StatusBadge status={inspectedItem.status} eppStatuses={inspectedItem.epp_statuses} />
                  <Pill>{tldOf(inspectedItem.name) || "—"}</Pill>
                </div>
                <p className="mt-2 text-[12px] text-ink-muted">{STATUS_HINT[inspectedItem.status] ?? ""}</p>

                <dl className="mt-3 space-y-1.5 text-[12px]">
                  <div className="flex justify-between gap-3">
                    <dt className="text-ink-muted">注册商</dt>
                    <dd className="truncate text-right" title={inspectedItem.registrar}>{registrarShortName(inspectedItem.registrar) || "—"}</dd>
                  </div>
                  <div className="flex justify-between gap-3">
                    <dt className="text-ink-muted">到期</dt>
                    <dd className="tabular text-right" title={formatDate(inspectedItem.expiry_date)}>{formatExpiryRelative(inspectedItem.expiry_date)}</dd>
                  </div>
                  <div className="flex justify-between gap-3">
                    <dt className="text-ink-muted">最后查询</dt>
                    <dd className="tabular text-right">{formatRelative(inspectedItem.last_checked)}</dd>
                  </div>
                </dl>

                {(() => {
                  const window = predictReleaseWindow(inspectedItem.name, inspectedItem.expiry_date);
                  if (!window) return null;
                  return (
                    <div className="mt-3 rounded-card border border-line p-3">
                      <p className="text-[12px] text-ink">释放预测</p>
                      <p className="tabular mt-1 text-[12px] text-ink-muted">{window.label}</p>
                      <p className="tabular mt-0.5 text-[11px] text-ink-faint">
                        {window.daysRemaining >= 0 ? `还有约 ${window.daysRemaining} 天` : `已过预测窗口 ${Math.abs(window.daysRemaining)} 天`}
                      </p>
                    </div>
                  );
                })()}

                <div className="mt-3 flex flex-wrap gap-2">
                  <button type="button" className="btn h-8 text-[12px]" onClick={() => void setReminder(inspectedItem)}>
                    {inspectedItem.notify === false ? "恢复提醒" : "设为提醒"}
                  </button>
                  <RegistrarLookupMenu domain={inspectedItem.name} />
                </div>

                <p className="mt-3 border-t border-line pt-2 text-[10px] leading-4 text-ink-faint">
                  按 TLD 通用删除节奏推算，不是注册局承诺。
                </p>
              </aside>
            )}
          </div>

          <ul className="space-y-2 md:hidden">
            {items.map((item) => (
              <MobileCard
                key={item.name}
                item={item}
                busy={busy === item.name}
                onOpen={() => setOpenDomain(item.name)}
                onCheck={() => runCheck(item.name)}
                onHistory={() => navigate(`/history?domain=${encodeURIComponent(item.name)}`)}
                onRemind={() => void setReminder(item)}
              />
            ))}
          </ul>
        </>
      )}

      <DomainDrawer
        domain={openDomain}
        domains={items.map((item) => item.name)}
        onNavigate={setOpenDomain}
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
        Math.abs(days) <= 7
          ? "text-danger"
          : Math.abs(days) <= 30
            ? "text-warning"
            : "text-neutral",
      )}
    >
      {overdue ? `已过期 ${-days} 天` : `${days} 天后`}
    </span>
  );
}

function DropBoard({ counts }: { counts: Map<string, number> }) {
  const stageRanges = ["0–45 天", "到期日", "约 30 天", "约 5 天", "实时"];
  return (
    <Card
      title="掉落阶段"
      action={<Pill title="从宽限期到可注册的五个观察阶段">5 阶段</Pill>}
      bodyClassName="p-0"
    >
      <div className="relative overflow-hidden px-6 py-6 sm:px-8 sm:py-8">
        <div
          className="absolute bottom-7 left-8 top-7 w-px bg-line md:hidden"
          aria-hidden="true"
        />
        <svg
          className="pointer-events-none absolute left-8 right-8 top-9 hidden h-1.5 w-[calc(100%-4rem)] md:block"
          viewBox="0 0 100 4"
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          <line x1="5" y1="2" x2="95" y2="2" stroke="rgb(var(--line))" strokeWidth="1" />
        </svg>
        <ol className="relative grid gap-4 md:grid-cols-5 md:gap-2">
          {DROP_STAGES.map(({ status, hint }) => {
            const count = counts.get(status) ?? 0;
            const active = count > 0;
            return (
              <li
                key={status}
                className="flex items-center gap-3 md:flex-col md:gap-2 md:text-center"
                aria-label={`${STATUS_LABELS[status]}，${count} 个`}
              >
                <span
                  className={cx(
                    "z-10 flex h-8 w-8 shrink-0 items-center justify-center rounded-full border text-[12px] font-semibold tabular transition-colors",
                    active
                      ? "border-accent bg-accent text-on-accent"
                      : "border-line bg-surface-raised text-ink-faint",
                  )}
                >
                  {count}
                </span>
                <span className="min-w-0">
                  <span className="block">
                    <StatusBadge status={status} />
                  </span>
                  <span className="mt-1 block text-[11px] text-ink-faint">{hint}</span>
                  <span className="mt-0.5 block font-mono text-[10px] text-ink-faint">{stageRanges[DROP_STAGES.findIndex((item) => item.status === status)]}</span>
                </span>
              </li>
            );
          })}
        </ol>
      </div>
    </Card>
  );
}

/**
 * 看板卡片。倒计时是最大的元素——这一页要回答的是"还有多久能动手"，
 * 域名本身反而次要；注册商与 TLD 只给短名，五步点标出当前走到哪一阶段。
 */
function KanbanCard({
  item,
  active,
  onSelect,
  onOpen,
  onRemind,
}: {
  item: DomainInfo;
  active: boolean;
  onSelect: () => void;
  onOpen: () => void;
  onRemind: () => void;
}) {
  const window = predictReleaseWindow(item.name, item.expiry_date);
  const days = item.status === "available" ? null : window?.daysRemaining ?? daysUntil(item.expiry_date);
  const stageIndex = DROP_STAGES.findIndex((stage) => stage.status === item.status);

  return (
    <div
      className={cx(
        "card cursor-pointer p-3 transition-colors hover:border-accent",
        active && "border-accent",
      )}
      onClick={onSelect}
      role="button"
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
    >
      <div className="tabular font-mono text-[22px] leading-none text-ink">
        {item.status === "available" ? (
          <span className="text-action">可注册</span>
        ) : days === null ? (
          "—"
        ) : days >= 0 ? (
          <>D-{days}</>
        ) : (
          <span className="text-neg">D+{Math.abs(days)}</span>
        )}
      </div>
      <button
        type="button"
        className="mono mt-2 block w-full truncate text-left text-[13px] text-ink hover:text-accent hover:underline"
        onClick={(event) => { event.stopPropagation(); onOpen(); }}
        title={item.name}
      >
        {item.name}
      </button>
      <p className="mt-1 flex items-center gap-1.5 text-[11px] text-ink-faint">
        <span className="truncate" title={item.registrar}>{registrarShortName(item.registrar) || "—"}</span>
        <span aria-hidden="true">·</span>
        <span className="shrink-0">{tldOf(item.name)}</span>
      </p>
      <div className="mt-2 flex items-center justify-between gap-2">
        {/* 五步点：一眼看出这个域名走到掉落流程的第几段。 */}
        <span className="flex gap-1" aria-label={`掉落流程第 ${stageIndex + 1} / ${DROP_STAGES.length} 阶段`}>
          {DROP_STAGES.map((stage, index) => (
            <span
              key={stage.status}
              className={cx(
                "h-1 w-1.5 rounded-full",
                index < stageIndex ? "bg-line" : index === stageIndex ? "bg-action" : "bg-line/50",
              )}
              aria-hidden="true"
            />
          ))}
        </span>
        <button
          type="button"
          className="text-[11px] text-ink-faint hover:text-accent"
          onClick={(event) => { event.stopPropagation(); onRemind(); }}
        >
          {item.notify === false ? "恢复提醒" : "提醒"}
        </button>
      </div>
    </div>
  );
}

function MobileCard({
  item,
  busy,
  onOpen,
  onCheck,
  onHistory,
  onRemind,
}: {
  item: DomainInfo;
  busy: boolean;
  onOpen: () => void;
  onCheck: () => void;
  onHistory: () => void;
  onRemind: () => void;
}) {
  const release = predictReleaseWindow(item.name, item.expiry_date);
  return (
    <li className="card p-3">
      <DomainName name={item.name} onClick={onOpen} className="block w-full" />
      <div className="mt-1.5 flex flex-wrap items-center gap-2">
        <StatusBadge status={item.status} eppStatuses={item.epp_statuses} />
        {item.review?.required && <ReviewIndicator explanation={item.review.explanation} />}
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
      {release && <p className="mt-2 rounded-md bg-info/8 px-2 py-1.5 text-[11px] text-info" title="按 TLD 通用删除节奏推算，不是注册局承诺">释放预测：{release.label} · {release.daysRemaining <= 0 ? "已进入预测窗口" : `${release.daysRemaining} 天后`}</p>}
      <div className="mt-3 grid grid-cols-3 gap-2">
        <button type="button" className="btn min-h-11 text-[12px]" onClick={onRemind}>{item.notify === false ? "恢复提醒" : "忽略提醒"}</button>
        <button type="button" className="btn min-h-11 text-[12px]" onClick={onHistory}>状态时间线</button>
        <button type="button" className="btn btn-primary min-h-11 text-[12px]" onClick={onCheck} disabled={busy}>{busy ? "查询中…" : "立即检查"}</button>
      </div>
    </li>
  );
}
