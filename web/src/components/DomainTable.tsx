import { useCallback, useEffect, useRef, useState } from "react";
import type { DomainInfo } from "../lib/api";
import {
  DomainName,
  Pill,
  ReviewIndicator,
  StatusBadge,
  cx,
  useStatusChangeHighlights,
} from "./ui";
import { formatDate, formatDateTime, formatRelative, providerLabel } from "../lib/format";

export type DomainColumn =
  | "status"
  | "registrar"
  | "expiry"
  | "provider"
  | "last_checked"
  | "next_check";

export type DomainSort = "name" | "status" | "expiry" | "last_checked" | "next_check" | "";

interface Props {
  domains: DomainInfo[];
  selected: Set<string>;
  visibleColumns: Set<DomainColumn>;
  sort: DomainSort;
  order: "asc" | "desc";
  onSort: (sort: DomainSort) => void;
  onToggle: (name: string) => void;
  onToggleAll: (checked: boolean) => void;
  onOpen: (name: string) => void;
  onCheck: (name: string) => void;
  onDelete: (name: string) => void;
  onHistory: (name: string) => void;
  busy: string | null;
}

export function DomainTable(props: Props) {
  const highlighted = useStatusChangeHighlights(
    props.domains,
    (item) => item.name,
    (item) => item.status,
  );

  return (
    <>
      <div className="hidden min-w-0 md:block">
        <DesktopTable {...props} highlighted={highlighted} />
      </div>
      <div className="md:hidden">
        <MobileList {...props} highlighted={highlighted} />
      </div>
    </>
  );
}

function DesktopTable({
  domains,
  selected,
  visibleColumns,
  sort,
  order,
  onSort,
  onToggle,
  onToggleAll,
  onOpen,
  onCheck,
  onDelete,
  onHistory,
  busy,
  highlighted,
}: Props & { highlighted: Set<string> }) {
  const allSelected = domains.length > 0 && domains.every((item) => selected.has(item.name));
  const topScroll = useRef<HTMLDivElement>(null);
  const bottomScroll = useRef<HTMLDivElement>(null);
  const table = useRef<HTMLTableElement>(null);
  const [tableWidth, setTableWidth] = useState(0);

  const syncScroll = useCallback((source: HTMLDivElement, target: HTMLDivElement | null) => {
    if (target && Math.abs(target.scrollLeft - source.scrollLeft) > 1) target.scrollLeft = source.scrollLeft;
  }, []);

  useEffect(() => {
    const update = () => setTableWidth(table.current?.scrollWidth ?? 0);
    update();
    const observer = new ResizeObserver(update);
    if (table.current) observer.observe(table.current);
    return () => observer.disconnect();
  }, [domains, visibleColumns]);

  return (
    <div className="min-w-0 space-y-2">
      <div
        ref={topScroll}
        className="table-top-scroll"
        onScroll={(event) => syncScroll(event.currentTarget, bottomScroll.current)}
        aria-label="表格顶部横向滚动条"
      >
        <div style={{ width: `${tableWidth}px`, height: 1 }} />
      </div>
      <div
        ref={bottomScroll}
        className="table-scroll card"
        onScroll={(event) => syncScroll(event.currentTarget, topScroll.current)}
      >
        <table ref={table} className="data-table-refined w-full min-w-[1040px] table-fixed text-left">
          <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
            <tr>
              <th className="w-11 px-3 py-2">
                <input
                  type="checkbox"
                  checked={allSelected}
                  onChange={(event) => onToggleAll(event.target.checked)}
                  aria-label="全选"
                />
              </th>
              <SortableHeader label="域名" field="name" width="w-[190px]" sort={sort} order={order} onSort={onSort} />
              {visibleColumns.has("status") && <SortableHeader label="状态" field="status" width="w-[132px]" sort={sort} order={order} onSort={onSort} />}
              {visibleColumns.has("registrar") && <th className="w-[150px] px-3 py-2 font-medium">注册商</th>}
              {visibleColumns.has("expiry") && <SortableHeader label="到期时间" field="expiry" width="w-[122px]" sort={sort} order={order} onSort={onSort} />}
              {visibleColumns.has("provider") && <th className="w-[150px] px-3 py-2 font-medium">查询来源</th>}
              {visibleColumns.has("last_checked") && <SortableHeader label="最后查询" field="last_checked" width="w-[112px]" sort={sort} order={order} onSort={onSort} />}
              {visibleColumns.has("next_check") && <SortableHeader label="下次查询" field="next_check" width="w-[112px]" sort={sort} order={order} onSort={onSort} />}
              <th className="sticky-action-column sticky right-0 z-20 w-[178px] bg-surface-muted px-3 py-2 text-right font-medium">操作</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line text-[13px]">
            {domains.map((item) => (
              <tr
                key={item.name}
                draggable
                onDragStart={(event) => setDragData(event, item.name, selected)}
                className={cx("group hover:bg-surface-muted/60", highlighted.has(item.name) && "status-change-highlight")}
              >
                <td className="px-3 py-2">
                  <input type="checkbox" checked={selected.has(item.name)} onChange={() => onToggle(item.name)} aria-label={`选择 ${item.name}`} />
                </td>
                <td className="min-w-0 px-3 py-2">
                  <div className="flex min-w-0 items-center gap-2">
                    <DomainName name={item.name} favorite={item.favorite} onClick={() => onOpen(item.name)} className="min-w-0 flex-1" />
                    {item.review?.required && <ReviewIndicator explanation={item.review.explanation} />}
                  </div>
                </td>
                {visibleColumns.has("status") && (
                  <td className="px-3 py-2">
                    <div className="flex items-center gap-1.5">
                      <StatusBadge status={item.status} eppStatuses={item.epp_statuses} />
                      {item.cached && <Pill title="本次结果来自查询缓存">缓存</Pill>}
                    </div>
                  </td>
                )}
                {visibleColumns.has("registrar") && <td className="truncate px-3 py-2 text-ink-muted" title={item.registrar}>{item.registrar || "—"}</td>}
                {visibleColumns.has("expiry") && <td className="tabular whitespace-nowrap px-3 py-2 text-ink-muted">{formatDate(item.expiry_date)}</td>}
                {visibleColumns.has("provider") && <td className="truncate whitespace-nowrap px-3 py-2 text-ink-muted" title={providerLabel(item.query_method)}>{providerLabel(item.query_method)}</td>}
                {visibleColumns.has("last_checked") && <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">{formatRelative(item.last_checked)}</td>}
                {visibleColumns.has("next_check") && <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">{formatRelative(item.next_check_at)}</td>}
                <td className="sticky-action-column sticky right-0 z-10 whitespace-nowrap bg-surface px-3 py-2 text-right group-hover:bg-surface-muted">
                  <button type="button" className="btn btn-ghost h-8 px-2 text-[12px]" onClick={() => onHistory(item.name)} title="打开状态时间线">时间线</button>
                  <button type="button" className="btn btn-ghost h-8 px-2 text-[12px]" onClick={() => onCheck(item.name)} disabled={busy === item.name}>{busy === item.name ? "查询中…" : "检查"}</button>
                  <button type="button" className="btn btn-ghost h-8 w-8 px-0 text-danger" onClick={() => onDelete(item.name)} aria-label={`删除 ${item.name}`} title="删除">×</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SortableHeader({ label, field, width, sort, order, onSort }: { label: string; field: Exclude<DomainSort, "">; width: string; sort: DomainSort; order: "asc" | "desc"; onSort: (sort: DomainSort) => void }) {
  const active = sort === field;
  return (
    <th className={cx(width, "px-3 py-2 font-medium")} aria-sort={active ? (order === "asc" ? "ascending" : "descending") : "none"}>
      <button type="button" className={cx("inline-flex min-h-8 items-center gap-1", active && "text-accent")} onClick={() => onSort(field)}>
        {label}<span aria-hidden="true">{active ? (order === "asc" ? "↑" : "↓") : "↕"}</span>
      </button>
    </th>
  );
}

function MobileList({ domains, selected, onToggle, onOpen, onCheck, onDelete, onHistory, busy, highlighted }: Props & { highlighted: Set<string> }) {
  return (
    <ul className="space-y-3">
      {domains.map((item) => (
        <MobileDomainCard
          key={item.name}
          item={item}
          checked={selected.has(item.name)}
          busy={busy === item.name}
          highlighted={highlighted.has(item.name)}
          onToggle={() => onToggle(item.name)}
          onOpen={() => onOpen(item.name)}
          onCheck={() => onCheck(item.name)}
          onDelete={() => onDelete(item.name)}
          onHistory={() => onHistory(item.name)}
        />
      ))}
    </ul>
  );
}

function MobileDomainCard({ item, checked, busy, highlighted, onToggle, onOpen, onCheck, onDelete, onHistory }: { item: DomainInfo; checked: boolean; busy: boolean; highlighted: boolean; onToggle: () => void; onOpen: () => void; onCheck: () => void; onDelete: () => void; onHistory: () => void }) {
  const [menuOpen, setMenuOpen] = useState(false);
  return (
    <li className={cx("card p-4", highlighted && "status-change-highlight")}>
      <div className="flex min-w-0 items-center gap-2">
        <input type="checkbox" checked={checked} onChange={onToggle} aria-label={`选择 ${item.name}`} />
        <DomainName name={item.name} favorite={item.favorite} onClick={onOpen} className="min-w-0 flex-1 text-[14px]" />
        {item.review?.required && <ReviewIndicator explanation={item.review.explanation} />}
        <StatusBadge status={item.status} eppStatuses={item.epp_statuses} />
      </div>
      <dl className="mt-4 grid grid-cols-2 gap-x-5 gap-y-3 text-[12px]">
        <Row label="到期" value={formatDate(item.expiry_date)} />
        <Row label="注册商" value={item.registrar || "—"} />
        <Row label="查询源" value={providerLabel(item.query_method)} />
        <Row label="最后查询" value={formatRelative(item.last_checked)} />
      </dl>
      <div className="mt-4 flex gap-2 border-t border-line pt-3">
        <button type="button" className="btn btn-primary min-h-11 flex-1 text-[13px]" onClick={onCheck} disabled={busy}>{busy ? "查询中…" : "立即检查"}</button>
        <div className="relative">
          <button type="button" className="btn min-h-11 min-w-11 px-0 text-[18px]" onClick={() => setMenuOpen((value) => !value)} aria-label={`${item.name} 更多操作`} aria-expanded={menuOpen}>⋯</button>
          {menuOpen && (
            <div className="absolute bottom-12 right-0 z-20 w-36 rounded-card border border-line bg-surface-raised p-1 shadow-lg">
              <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[13px] hover:bg-surface-muted" onClick={() => { setMenuOpen(false); onOpen(); }}>查看详情</button>
              <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[13px] hover:bg-surface-muted" onClick={() => { setMenuOpen(false); onHistory(); }}>状态时间线</button>
              <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[13px] text-danger hover:bg-danger/12" onClick={() => { setMenuOpen(false); onDelete(); }}>删除域名</button>
            </div>
          )}
        </div>
      </div>
      <p className="sr-only">{formatDateTime(item.last_checked)}</p>
    </li>
  );
}

function setDragData(event: React.DragEvent, name: string, selected: Set<string>) {
  const names = selected.has(name) ? Array.from(selected) : [name];
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData("application/x-domainhunter-domains", JSON.stringify(names));
  event.dataTransfer.setData("text/plain", names.join("\n"));
}

function Row({ label, value }: { label: string; value: string }) {
  return <div className="min-w-0"><dt className="text-ink-faint">{label}</dt><dd className="tabular mt-0.5 truncate text-ink-muted" title={value}>{value}</dd></div>;
}
