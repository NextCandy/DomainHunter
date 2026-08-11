import type { DomainInfo } from "../lib/api";
import { DomainName, Pill, StatusBadge, cx, useStatusChangeHighlights } from "./ui";
import { formatDate, formatDateTime, formatRelative, providerLabel } from "../lib/format";

interface Props {
  domains: DomainInfo[];
  selected: Set<string>;
  onToggle: (name: string) => void;
  onToggleAll: (checked: boolean) => void;
  onOpen: (name: string) => void;
  onCheck: (name: string) => void;
  onDelete: (name: string) => void;
  busy: string | null;
}

/**
 * 桌面端用表格，窄屏（<768px）自动切换成卡片列表 —— 表格在手机上缩小后
 * 只会变得不可读，所以这里不是简单缩放而是换一种布局。
 */
export function DomainTable(props: Props) {
  const highlighted = useStatusChangeHighlights(
    props.domains,
    (item) => item.name,
    (item) => item.status,
  );

  return (
    <>
      <div className="hidden md:block">
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
  onToggle,
  onToggleAll,
  onOpen,
  onCheck,
  onDelete,
  busy,
  highlighted,
}: Props & { highlighted: Set<string> }) {
  const allSelected = domains.length > 0 && domains.every((item) => selected.has(item.name));

  return (
    <div className="table-scroll card">
      <table className="w-full min-w-[960px] text-left">
        <thead className="bg-surface-muted text-[11px] uppercase tracking-wide text-ink-muted">
          <tr>
            <th className="w-9 px-3 py-2">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={(event) => onToggleAll(event.target.checked)}
                aria-label="全选"
              />
            </th>
            <th className="px-3 py-2 font-medium">域名</th>
            <th className="px-3 py-2 font-medium">状态</th>
            <th className="px-3 py-2 font-medium">注册商</th>
            <th className="px-3 py-2 font-medium">到期时间</th>
            <th className="px-3 py-2 font-medium">查询来源</th>
            <th className="px-3 py-2 font-medium">最后查询</th>
            <th className="px-3 py-2 font-medium">下次查询</th>
            <th className="px-3 py-2 text-right font-medium">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line text-[13px]">
          {domains.map((item) => (
            <tr
              key={item.name}
              draggable
              onDragStart={(event) => setDragData(event, item.name, selected)}
              className={cx("hover:bg-surface-muted/60", highlighted.has(item.name) && "status-change-highlight")}
            >
              <td className="px-3 py-2">
                <input
                  type="checkbox"
                  checked={selected.has(item.name)}
                  onChange={() => onToggle(item.name)}
                  aria-label={`选择 ${item.name}`}
                />
              </td>
              <td className="px-3 py-2">
                <DomainName name={item.name} favorite={item.favorite} onClick={() => onOpen(item.name)} />
              </td>
              <td className="px-3 py-2">
                <div className="flex flex-wrap items-center gap-1.5">
                  <StatusBadge status={item.status} eppStatuses={item.epp_statuses} />
                  {item.cached && <Pill title="本次结果来自查询缓存">cached: true</Pill>}
                </div>
              </td>
              <td className="max-w-[180px] truncate px-3 py-2 text-ink-muted" title={item.registrar}>
                {item.registrar || "—"}
              </td>
              <td className="tabular whitespace-nowrap px-3 py-2 text-ink-muted">
                {formatDate(item.expiry_date)}
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
                {providerLabel(item.query_method)}
              </td>
              <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">
                {formatRelative(item.last_checked)}
              </td>
              <td className="tabular whitespace-nowrap px-3 py-2 text-ink-faint">
                {formatRelative(item.next_check_at)}
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-right">
                <button
                  type="button"
                  className="btn btn-ghost h-7 px-2 text-[12px]"
                  onClick={() => onCheck(item.name)}
                  disabled={busy === item.name}
                >
                  {busy === item.name ? "查询中…" : "立即检查"}
                </button>
                <button
                  type="button"
                  className="btn btn-ghost h-7 px-2 text-[12px] text-red-600 dark:text-red-400"
                  onClick={() => onDelete(item.name)}
                >
                  删除
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function MobileList({
  domains,
  selected,
  onToggle,
  onOpen,
  onCheck,
  onDelete,
  busy,
  highlighted,
}: Props & { highlighted: Set<string> }) {
  return (
    <ul className="space-y-2">
      {domains.map((item) => (
        <li
          key={item.name}
          draggable
          onDragStart={(event) => setDragData(event, item.name, selected)}
          className={cx("card p-3", highlighted.has(item.name) && "status-change-highlight")}
        >
          <div className="flex items-start gap-2">
            <input
              type="checkbox"
              className="mt-1"
              checked={selected.has(item.name)}
              onChange={() => onToggle(item.name)}
              aria-label={`选择 ${item.name}`}
            />
            <div className="min-w-0 flex-1">
              <DomainName name={item.name} favorite={item.favorite} onClick={() => onOpen(item.name)} />
              <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-ink-muted">
                <StatusBadge status={item.status} eppStatuses={item.epp_statuses} />
                {item.cached && <Pill title="本次结果来自查询缓存">cached: true</Pill>}
                <span className="truncate">{item.registrar || "—"}</span>
              </div>
              <dl className="mt-2 grid grid-cols-2 gap-x-3 gap-y-1 text-[12px] text-ink-faint">
                <Row label="到期" value={formatDate(item.expiry_date)} />
                <Row label="来源" value={providerLabel(item.query_method)} />
                <Row label="最后查询" value={formatRelative(item.last_checked)} />
                <Row label="下次查询" value={formatRelative(item.next_check_at)} />
              </dl>
              <div className="mt-2 flex gap-2">
                <button
                  type="button"
                  className="btn h-7 flex-1 text-[12px]"
                  onClick={() => onCheck(item.name)}
                  disabled={busy === item.name}
                >
                  {busy === item.name ? "查询中…" : "立即检查"}
                </button>
                <button
                  type="button"
                  className={cx("btn h-7 px-3 text-[12px] text-red-600 dark:text-red-400")}
                  onClick={() => onDelete(item.name)}
                >
                  删除
                </button>
              </div>
            </div>
          </div>
          <p className="sr-only">{formatDateTime(item.last_checked)}</p>
        </li>
      ))}
    </ul>
  );
}

function setDragData(event: React.DragEvent, name: string, selected: Set<string>) {
  const names = selected.has(name) ? Array.from(selected) : [name];
  event.dataTransfer.effectAllowed = "move";
  event.dataTransfer.setData("application/x-domainhunter-domains", JSON.stringify(names));
  event.dataTransfer.setData("text/plain", names.join("\n"));
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex gap-1">
      <dt>{label}</dt>
      <dd className="tabular truncate text-ink-muted">{value}</dd>
    </div>
  );
}
