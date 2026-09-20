import type { DomainInfo } from "./api";
import { STATUS_LABELS } from "./format";

/**
 * 把即将到期的域名导出成 .ics 日历。
 *
 * 到期这件事的自然归宿是日历而不是一个网页列表——续费要提前安排，
 * 而人看的是自己的日历。这里只用已经拉到的域名数据本地生成，
 * 不需要后端新端点，也不会把域名清单发到任何第三方。
 *
 * 每个域名一个全天事件，UID 用「域名 + 到期日」保证重复导入不会生成副本。
 */

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

/** 全天事件用 DATE 值（YYYYMMDD），按本地日历取，和页面上显示的日期一致。 */
function toDateValue(date: Date): string {
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}`;
}

function toStamp(date: Date): string {
  return (
    `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}` +
    `T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}Z`
  );
}

/** RFC 5545 要求转义这几个字符，否则带逗号的注册商名会把字段截断。 */
function escapeText(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/;/g, "\\;").replace(/,/g, "\\,").replace(/\r?\n/g, "\\n");
}

/** 折行到 75 字节以内是规范要求；中文按字符折更安全，继续行以一个空格开头。 */
function foldLine(line: string): string {
  if (line.length <= 70) return line;
  const chunks: string[] = [];
  let rest = line;
  chunks.push(rest.slice(0, 70));
  rest = rest.slice(70);
  while (rest.length > 0) {
    chunks.push(` ${rest.slice(0, 69)}`);
    rest = rest.slice(69);
  }
  return chunks.join("\r\n");
}

export interface ExpiryCalendarOptions {
  /** 只导出未来这么多天内到期的域名。 */
  days?: number;
}

/** 返回 .ics 文本；没有符合条件的域名时返回空串，调用方据此提示。 */
export function buildExpiryICS(domains: DomainInfo[], options: ExpiryCalendarOptions = {}): string {
  const days = options.days ?? 60;
  const now = new Date();
  const today = new Date(now);
  today.setHours(0, 0, 0, 0);
  const horizon = new Date(today);
  horizon.setDate(today.getDate() + days);

  const events: string[] = [];
  for (const item of domains) {
    if (!item.expiry_date) continue;
    const expiry = new Date(item.expiry_date);
    if (Number.isNaN(expiry.getTime())) continue;
    const day = new Date(expiry);
    day.setHours(0, 0, 0, 0);
    if (day < today || day > horizon) continue;

    const end = new Date(day);
    end.setDate(day.getDate() + 1);
    const statusLabel = STATUS_LABELS[item.status] ?? item.status;
    const description = [
      `状态：${statusLabel}`,
      item.registrar ? `注册商：${item.registrar}` : "",
      "由 DomainHunter 导出，仅供提醒参考。",
    ]
      .filter(Boolean)
      .join("\n");

    events.push(
      [
        "BEGIN:VEVENT",
        foldLine(`UID:${escapeText(item.name)}-${toDateValue(day)}@domainhunter`),
        `DTSTAMP:${toStamp(now)}`,
        `DTSTART;VALUE=DATE:${toDateValue(day)}`,
        `DTEND;VALUE=DATE:${toDateValue(end)}`,
        foldLine(`SUMMARY:${escapeText(`${item.name} 到期（${statusLabel}）`)}`),
        foldLine(`DESCRIPTION:${escapeText(description)}`),
        "TRANSP:TRANSPARENT",
        "END:VEVENT",
      ].join("\r\n"),
    );
  }

  if (events.length === 0) return "";

  return [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//DomainHunter//Domain Expiry//CN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    foldLine(`X-WR-CALNAME:${escapeText(`DomainHunter 到期提醒（未来 ${days} 天）`)}`),
    ...events,
    "END:VCALENDAR",
    "",
  ].join("\r\n");
}
