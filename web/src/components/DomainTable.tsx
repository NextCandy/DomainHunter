import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { DomainInfo } from "../lib/api";
import {
  DomainName,
  Pill,
  ReviewIndicator,
  StatusBadge,
  cx,
  useDensity,
  useStatusChangeHighlights,
} from "./ui";
import { formatDate, formatDateTime, formatRelative, providerLabel } from "../lib/format";

export type DomainColumn =
  | "status"
  | "registrar"
  | "expiry"
  | "provider"
  | "ai_score"
  | "last_checked"
  | "next_check";

export type DomainSort = "name" | "status" | "expiry" | "last_checked" | "next_check" | "ai_score" | "";

type TableColumnKey = "select" | "domain" | DomainColumn | "actions";

export const DOMAIN_TABLE_DEFAULT_WIDTHS: Record<TableColumnKey, number> = {
  select: 40,
  domain: 180,
  status: 120,
  registrar: 160,
  // Inter 的数字比之前的系统字体宽，日期列留出余量，避免字体回退时被裁。
  expiry: 122,
  provider: 100,
  ai_score: 90,
  last_checked: 118,
  next_check: 120,
  // 三个 32px 图标按钮 + 列内左右 padding，低于 108 会把最右边的删除按钮挤出列外。
  actions: 112,
};

const OPTIONAL_COLUMNS: DomainColumn[] = [
  "status",
  "registrar",
  "expiry",
  "provider",
  "ai_score",
  "last_checked",
  "next_check",
];

const DOMAIN_TABLE_WIDTHS_KEY = "dh-domains-column-widths-v1";

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
  keyboardIndex?: number;
}

export function DomainTable(props: Props) {
  const highlighted = useStatusChangeHighlights(
    props.domains,
    (item) => item.name,
    (item) => item.status,
  );

  return (
    <>
      <div className="hidden min-w-0 lg:block">
        <DesktopTable {...props} highlighted={highlighted} />
      </div>
      <div className="lg:hidden">
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
  keyboardIndex,
  highlighted,
}: Props & { highlighted: Set<string> }) {
  const allSelected = domains.length > 0 && domains.every((item) => selected.has(item.name));
  const { density } = useDensity();
  const topScroll = useRef<HTMLDivElement>(null);
  const bottomScroll = useRef<HTMLDivElement>(null);
  const table = useRef<HTMLTableElement>(null);
  const scrollFrame = useRef<number | null>(null);
  const [tableWidth, setTableWidth] = useState(0);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(620);
  const [columnWidths, setColumnWidths] = useState<Record<TableColumnKey, number>>(() => readColumnWidths());
  const resizeRef = useRef<{ column: TableColumnKey; startX: number; startWidth: number } | null>(null);

  const rowHeight = density === "dense" ? 36 : density === "compact" ? 44 : 56;
  // Keep the normal table DOM small when the user selects “全部”. The server
  // can return 1,000+ records, but only the visible window is laid out.
  const virtualized = domains.length > 240;
  const overscan = 10;
  const firstVisible = virtualized ? Math.max(0, Math.floor(scrollTop / rowHeight) - overscan) : 0;
  const visibleCount = virtualized ? Math.ceil(viewportHeight / rowHeight) + overscan * 2 : domains.length;
  const lastVisible = virtualized ? Math.min(domains.length, firstVisible + visibleCount) : domains.length;
  const visibleDomains = virtualized ? domains.slice(firstVisible, lastVisible) : domains;
  const columnCount = 3 + visibleColumns.size;
  // 表格最小宽度必须跟着实际可见列走。写死一个总宽会在隐藏列后仍强制撑出
  // 横向滚动，也会在列宽被拖动后与 colgroup 对不上。
  const minTableWidth = useMemo(() => {
    let total = columnWidths.select + columnWidths.domain + columnWidths.actions;
    for (const column of OPTIONAL_COLUMNS) {
      if (visibleColumns.has(column)) total += columnWidths[column];
    }
    return total;
  }, [columnWidths, visibleColumns]);

  const syncScroll = useCallback((source: HTMLDivElement, target: HTMLDivElement | null) => {
    if (target && Math.abs(target.scrollLeft - source.scrollLeft) > 1) target.scrollLeft = source.scrollLeft;
  }, []);

  useEffect(() => {
    const update = () => setTableWidth(table.current?.scrollWidth ?? 0);
    update();
    const observer = new ResizeObserver(update);
    if (table.current) observer.observe(table.current);
    return () => observer.disconnect();
  }, [domains, visibleColumns, virtualized, density]);

  useEffect(() => {
    const viewport = bottomScroll.current;
    if (!viewport) return;
    const observer = new ResizeObserver(() => setViewportHeight(viewport.clientHeight || 620));
    observer.observe(viewport);
    setViewportHeight(viewport.clientHeight || 620);
    return () => observer.disconnect();
  }, [virtualized]);

  useEffect(() => () => {
    if (scrollFrame.current != null) window.cancelAnimationFrame(scrollFrame.current);
  }, []);

  useEffect(() => {
    const onPointerMove = (event: PointerEvent) => {
      const resize = resizeRef.current;
      if (!resize) return;
      const nextWidth = Math.max(columnMinWidth(resize.column), Math.round(resize.startWidth + event.clientX - resize.startX));
      setColumnWidths((current) => ({ ...current, [resize.column]: nextWidth }));
    };
    const onPointerUp = () => {
      if (!resizeRef.current) return;
      resizeRef.current = null;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      setColumnWidths((current) => {
        try { localStorage.setItem(DOMAIN_TABLE_WIDTHS_KEY, JSON.stringify(current)); } catch { /* storage is optional */ }
        return current;
      });
    };
    document.addEventListener("pointermove", onPointerMove);
    document.addEventListener("pointerup", onPointerUp);
    return () => {
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", onPointerUp);
    };
  }, []);

  const startResize = useCallback((column: TableColumnKey, event: React.PointerEvent<HTMLSpanElement>) => {
    event.preventDefault();
    event.stopPropagation();
    resizeRef.current = { column, startX: event.clientX, startWidth: columnWidths[column] };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  }, [columnWidths]);

  const handleBottomScroll = useCallback((event: React.UIEvent<HTMLDivElement>) => {
    const source = event.currentTarget;
    syncScroll(source, topScroll.current);
    if (!virtualized || scrollFrame.current != null) return;
    scrollFrame.current = window.requestAnimationFrame(() => {
      scrollFrame.current = null;
      setScrollTop(source.scrollTop);
    });
  }, [syncScroll, virtualized]);

  const renderRow = useCallback((item: DomainInfo, index: number) => (
    <tr
      key={item.name}
      draggable
      onDragStart={(event) => setDragData(event, item.name, selected)}
      className={cx("group domain-row hover:bg-surface-muted/60", domainRowTone(item.status), highlighted.has(item.name) && "status-change-highlight", index === keyboardIndex && "outline outline-2 outline-accent outline-offset-[-2px]")}
      data-status={item.status}
    >
      <td className="px-2 py-2">
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
      {visibleColumns.has("ai_score") && <td className="tabular px-3 py-2"><span className={item.ai_quality_score == null ? "text-ink-faint" : item.ai_quality_score >= 80 ? "text-success" : item.ai_quality_score >= 60 ? "text-warning" : "text-danger"}>{item.ai_quality_score ?? "—"}</span></td>}
      {visibleColumns.has("last_checked") && <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">{formatRelative(item.last_checked)}</td>}
      {visibleColumns.has("next_check") && <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">{formatRelative(item.next_check_at)}</td>}
      <td className="sticky-action-column sticky right-0 z-10 whitespace-nowrap bg-surface px-1.5 py-2 text-right group-hover:bg-surface-muted">
        <button type="button" className="btn btn-ghost h-8 w-8 px-0" onClick={() => onHistory(item.name)} aria-label={`打开 ${item.name} 状态时间线`} title="状态时间线"><TimelineIcon /></button>
        <button type="button" className="btn btn-ghost h-8 w-8 px-0" onClick={() => onCheck(item.name)} disabled={busy === item.name} aria-label={`检查 ${item.name}`} title={busy === item.name ? "查询中" : "立即检查"}>{busy === item.name ? <SpinnerIcon /> : <RefreshIcon />}</button>
        <button type="button" className="btn btn-ghost h-8 w-8 px-0 text-danger" onClick={() => onDelete(item.name)} aria-label={`删除 ${item.name}`} title="删除"><DeleteIcon /></button>
      </td>
    </tr>
  ), [busy, highlighted, keyboardIndex, onCheck, onDelete, onHistory, onOpen, onToggle, selected, visibleColumns]);

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
        className={cx("table-scroll card", virtualized && "virtual-table-viewport")}
        onScroll={handleBottomScroll}
      >
        <table ref={table} className="data-table-refined w-full table-fixed text-left" style={{ minWidth: minTableWidth }}>
          <colgroup>
            <col style={{ width: columnWidths.select }} />
            <col style={{ width: columnWidths.domain }} />
            {visibleColumns.has("status") && <col style={{ width: columnWidths.status }} />}
            {visibleColumns.has("registrar") && <col style={{ width: columnWidths.registrar }} />}
            {visibleColumns.has("expiry") && <col style={{ width: columnWidths.expiry }} />}
            {visibleColumns.has("provider") && <col style={{ width: columnWidths.provider }} />}
            {visibleColumns.has("ai_score") && <col style={{ width: columnWidths.ai_score }} />}
            {visibleColumns.has("last_checked") && <col style={{ width: columnWidths.last_checked }} />}
            {visibleColumns.has("next_check") && <col style={{ width: columnWidths.next_check }} />}
            <col style={{ width: columnWidths.actions }} />
          </colgroup>
          <thead className="sticky top-0 z-20 bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
            <tr>
              <th className="relative px-2 py-2">
                <input
                  type="checkbox"
                  checked={allSelected}
                  onChange={(event) => onToggleAll(event.target.checked)}
                  aria-label="全选"
                />
                <ResizeHandle column="select" onPointerDown={startResize} />
              </th>
              <SortableHeader label="域名" field="name" column="domain" width={columnWidths.domain} sort={sort} order={order} onSort={onSort} onResize={startResize} />
              {visibleColumns.has("status") && <SortableHeader label="状态" field="status" column="status" width={columnWidths.status} sort={sort} order={order} onSort={onSort} onResize={startResize} />}
              {visibleColumns.has("registrar") && <PlainHeader label="注册商" column="registrar" width={columnWidths.registrar} onResize={startResize} />}
              {visibleColumns.has("expiry") && <SortableHeader label="到期时间" field="expiry" column="expiry" width={columnWidths.expiry} sort={sort} order={order} onSort={onSort} onResize={startResize} />}
              {visibleColumns.has("provider") && <PlainHeader label="查询来源" column="provider" width={columnWidths.provider} onResize={startResize} />}
              {visibleColumns.has("ai_score") && <SortableHeader label="AI 评分" field="ai_score" column="ai_score" width={columnWidths.ai_score} sort={sort} order={order} onSort={onSort} onResize={startResize} />}
              {visibleColumns.has("last_checked") && <SortableHeader label="最后查询" field="last_checked" column="last_checked" width={columnWidths.last_checked} sort={sort} order={order} onSort={onSort} onResize={startResize} />}
              {visibleColumns.has("next_check") && <SortableHeader label="下次查询" field="next_check" column="next_check" width={columnWidths.next_check} sort={sort} order={order} onSort={onSort} onResize={startResize} />}
              <th className="sticky-action-column sticky right-0 z-20 bg-surface-muted px-1.5 py-2 text-right font-medium" style={{ width: columnWidths.actions }}><span className="sr-only">操作</span><ResizeHandle column="actions" onPointerDown={startResize} /></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line text-[13px]">
            {virtualized && firstVisible > 0 && <tr className="virtual-table-spacer" aria-hidden="true"><td colSpan={columnCount} style={{ height: firstVisible * rowHeight }} /></tr>}
            {visibleDomains.map((item, offset) => renderRow(item, firstVisible + offset))}
            {virtualized && lastVisible < domains.length && <tr className="virtual-table-spacer" aria-hidden="true"><td colSpan={columnCount} style={{ height: (domains.length - lastVisible) * rowHeight }} /></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SortableHeader({ label, field, column, width, sort, order, onSort, onResize }: { label: string; field: Exclude<DomainSort, "">; column: TableColumnKey; width: number; sort: DomainSort; order: "asc" | "desc"; onSort: (sort: DomainSort) => void; onResize: (column: TableColumnKey, event: React.PointerEvent<HTMLSpanElement>) => void }) {
  const active = sort === field;
  return (
    <th className="relative px-3 py-2 font-medium" style={{ width }} aria-sort={active ? (order === "asc" ? "ascending" : "descending") : "none"}>
      <button type="button" className={cx("inline-flex min-h-8 items-center gap-1", active && "text-accent")} onClick={() => onSort(field)}>
        {label}<span aria-hidden="true">{active ? (order === "asc" ? "↑" : "↓") : "↕"}</span>
      </button>
      <ResizeHandle column={column} onPointerDown={onResize} />
    </th>
  );
}

function PlainHeader({ label, column, width, onResize }: { label: string; column: TableColumnKey; width: number; onResize: (column: TableColumnKey, event: React.PointerEvent<HTMLSpanElement>) => void }) {
  return <th className="relative px-3 py-2 font-medium" style={{ width }}>{label}<ResizeHandle column={column} onPointerDown={onResize} /></th>;
}

function ResizeHandle({ column, onPointerDown }: { column: TableColumnKey; onPointerDown: (column: TableColumnKey, event: React.PointerEvent<HTMLSpanElement>) => void }) {
  return <span role="separator" aria-label={`调整${column}列宽`} tabIndex={0} className="column-resize-handle" onPointerDown={(event) => onPointerDown(column, event)} />;
}

function MobileList({ domains, selected, onToggle, onOpen, onCheck, onDelete, onHistory, busy, highlighted }: Props & { highlighted: Set<string> }) {
  return (
    <ul className="virtual-mobile-list space-y-3">
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
    <li className={cx("card domain-row p-4", domainRowTone(item.status), highlighted && "status-change-highlight")} data-status={item.status}>
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

function columnMinWidth(column: TableColumnKey): number {
  // 操作列装着三个固定尺寸的图标按钮，比通用下限更窄就会溢出。
  return column === "actions" ? DOMAIN_TABLE_DEFAULT_WIDTHS.actions : 64;
}

function readColumnWidths(): Record<TableColumnKey, number> {
  try {
    const parsed = JSON.parse(localStorage.getItem(DOMAIN_TABLE_WIDTHS_KEY) ?? "null") as Partial<Record<TableColumnKey, number>> | null;
    if (parsed) {
      return Object.fromEntries(Object.entries(DOMAIN_TABLE_DEFAULT_WIDTHS).map(([key, value]) => {
        const stored = parsed[key as TableColumnKey];
        return [key, typeof stored === "number" ? Math.max(columnMinWidth(key as TableColumnKey), stored) : value];
      })) as Record<TableColumnKey, number>;
    }
  } catch { /* localStorage is optional */ }
  return { ...DOMAIN_TABLE_DEFAULT_WIDTHS };
}

function domainRowTone(status: string): string | undefined {
  if (status === "pending_delete" || status === "pendingDelete") return "domain-row-danger";
  if (status === "redemption") return "domain-row-warning-strong";
  if (status === "grace") return "domain-row-warning";
  if (status === "hold") return "domain-row-info";
  return undefined;
}

function TimelineIcon() {
  return <svg aria-hidden="true" className="h-3.5 w-3.5" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinecap="round"><circle cx="8" cy="8" r="5.5" /><path d="M8 4.75v3.5l2.25 1.25" /></svg>;
}

function RefreshIcon() {
  return <svg aria-hidden="true" className="h-3.5 w-3.5" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinecap="round"><path d="M13 5.75A5 5 0 1 0 13.25 9" /><path d="M10.5 2.75h3v3" /></svg>;
}

function SpinnerIcon() {
  return <span className="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-r-transparent" aria-label="查询中" role="status" />;
}

function DeleteIcon() {
  return <svg aria-hidden="true" className="h-3.5 w-3.5" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.35" strokeLinecap="round"><path d="M3.5 4.5h9M6 4.5V3h4v1.5M5 6.5v5M8 6.5v5M11 6.5v5M4.25 4.5l.5 8.25h6.5l.5-8.25" /></svg>;
}
