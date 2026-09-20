import { Link } from "react-router-dom";
import { cx } from "./ui";
import { daysUntil, formatDate } from "../lib/format";

/**
 * 到期紧迫度的统一色阶。越近越重，跨过今天就是红的。
 *
 * 表格里的热力条和概览的到期日历共用这一套分级，免得"7 天内"在两个地方
 * 一个是橙一个是红。颜色只用既有的语义 token：neg（红）/ warn（琥珀）。
 */
export function expiryTone(days: number | null): {
  /** 色块背景类 */
  fill: string;
  /** 文本色类 */
  text: string;
  /** 给屏幕阅读器和 title 的说明 */
  label: string;
} {
  if (days === null) return { fill: "bg-transparent", text: "text-ink-faint", label: "无到期日" };
  if (days < 0) return { fill: "bg-neg/45", text: "text-neg", label: `已过期 ${Math.abs(days)} 天` };
  if (days <= 7) return { fill: "bg-neg/45", text: "text-neg", label: `${days} 天后到期` };
  if (days <= 30) return { fill: "bg-warn/45", text: "text-warn", label: `${days} 天后到期` };
  if (days <= 60) return { fill: "bg-warn/25", text: "text-ink-muted", label: `${days} 天后到期` };
  return { fill: "bg-line", text: "text-ink-faint", label: `${days} 天后到期` };
}

/**
 * 表格里的到期热力条：一条 3px 的竖条，越近越红。
 * 让人扫一列就能看出哪几行该现在处理，不用逐行读数字。
 */
export function ExpiryHeatBar({ expiry }: { expiry?: string | null }) {
  const days = daysUntil(expiry);
  const tone = expiryTone(days);
  return (
    <span
      className={cx("inline-block h-4 w-[3px] shrink-0 rounded-full", tone.fill)}
      aria-hidden="true"
      title={tone.label}
    />
  );
}

const WEEKDAYS = ["一", "二", "三", "四", "五", "六", "日"];

function dayKey(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/**
 * 未来 N 天的到期热力日历。点一格跳到域名列表并按那天过滤。
 *
 * 概览原来只给一个"即将到期 60 天"的列表，看不出到期是均匀分布还是
 * 挤在某一周。日历把这件事变成一眼可见，也让"下周三有 5 个要续"
 * 这种判断不需要心算。
 */
export function ExpiryCalendar({
  counts,
  days = 30,
}: {
  counts: Record<string, number>;
  days?: number;
}) {
  const today = new Date();
  today.setHours(0, 0, 0, 0);

  const cells: Array<{ key: string; date: Date; count: number } | null> = [];
  // 周一开头：把第一天之前的空格补上，日期才会落在正确的星期列下。
  const leading = (today.getDay() + 6) % 7;
  for (let i = 0; i < leading; i += 1) cells.push(null);
  for (let i = 0; i < days; i += 1) {
    const date = new Date(today);
    date.setDate(today.getDate() + i);
    const key = dayKey(date);
    cells.push({ key, date, count: counts[key] ?? 0 });
  }

  const total = cells.reduce((sum, cell) => sum + (cell?.count ?? 0), 0);

  return (
    <div className="min-w-0">
      <div className="mb-1 grid grid-cols-7 gap-1 text-center text-[10px] text-ink-faint">
        {WEEKDAYS.map((label) => (
          <span key={label}>{label}</span>
        ))}
      </div>
      <div className="grid grid-cols-7 gap-1">
        {cells.map((cell, index) => {
          if (!cell) return <span key={`pad-${index}`} aria-hidden="true" />;
          const remaining = Math.round((cell.date.getTime() - today.getTime()) / 86_400_000);
          const tone = expiryTone(cell.count > 0 ? remaining : null);
          const title = `${formatDate(cell.date.toISOString())} · ${cell.count > 0 ? `${cell.count} 个域名到期` : "无到期"}`;

          if (cell.count === 0) {
            return (
              <span
                key={cell.key}
                className="flex h-8 items-center justify-center rounded-sm border border-line text-[11px] text-ink-faint"
                title={title}
              >
                {cell.date.getDate()}
              </span>
            );
          }
          return (
            <Link
              key={cell.key}
              to={`/domains?expiry=${cell.key}`}
              className={cx(
                "tabular flex h-8 flex-col items-center justify-center rounded-sm border border-transparent text-[11px] transition-colors hover:border-accent",
                tone.fill,
                tone.text,
              )}
              title={title}
              aria-label={title}
            >
              <span className="leading-none">{cell.date.getDate()}</span>
              <span className="text-[9px] leading-none opacity-80">{cell.count}</span>
            </Link>
          );
        })}
      </div>
      <p className="mt-2 text-[11px] text-ink-faint">
        {total > 0 ? `未来 ${days} 天共 ${total} 个域名到期，点日期查看当天清单` : `未来 ${days} 天没有域名到期`}
      </p>
    </div>
  );
}
