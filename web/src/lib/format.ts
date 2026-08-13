import type { DomainStatus } from "./api";

export const STATUS_LABELS: Record<DomainStatus, string> = {
  available: "可注册",
  registered: "已注册",
  grace: "宽限期",
  redemption: "赎回期",
  pending_delete: "待删除",
  expired: "已过期",
  transfer_locked: "转移锁定",
  hold: "Hold",
  unknown: "未知",
  error: "查询失败",
  skipped: "已跳过",
};

export const STATUS_ORDER: DomainStatus[] = [
  "available",
  "grace",
  "redemption",
  "pending_delete",
  "registered",
  "transfer_locked",
  "hold",
  "expired",
  "unknown",
  "error",
  "skipped",
];

/** Seline 状态色板：默认使用石色结构，只有可注册沿用青蓝信号。 */
export const STATUS_CLASSES: Record<DomainStatus, string> = {
  available:
    "bg-sky-wash/60 text-cyan-edge border-cyan-edge/40",
  registered: "bg-transparent text-ink-muted border-line",
  grace: "bg-surface-muted text-ink-muted border-stone-muted",
  redemption: "bg-surface-muted text-ink-muted border-stone-muted",
  pending_delete: "bg-surface-muted text-ink-muted border-stone-muted",
  expired: "bg-surface-muted text-ink-muted border-stone-muted",
  transfer_locked: "bg-surface-muted text-ink-muted border-stone-muted",
  hold: "bg-surface-muted text-ink-muted border-stone-muted",
  unknown: "bg-surface-muted text-ink-muted border-stone-muted",
  error: "bg-surface-muted text-ink-muted border-stone-muted",
  skipped: "bg-surface-muted text-ink-muted border-stone-muted",
};

export function hasTransferLock(statuses?: string[] | null): boolean {
  return Boolean(
    statuses?.some((status) => {
      const normalized = status.toLowerCase().replace(/[\s_-]+/g, "");
      return (
        normalized.includes("transferprohibited") ||
        normalized.includes("transferlock") ||
        normalized.includes("transferlocked")
      );
    }),
  );
}

export const CONFIDENCE_LABELS: Record<string, string> = {
  high: "高",
  medium: "中",
  low: "低",
};

export const PROVIDER_LABELS: Record<string, string> = {
  who_dat: "Pi who-dat",
  whois_domain_lookup: "Pi whois-domain-lookup",
  vercel_who_dat: "rdap.re（Vercel who-dat）",
  rdap: "RDAP",
  rdap_org: "rdap.org",
  whois: "WHOIS",
  ai_fallback: "默认 AI 兜底",
  whois_ls: "WHOIS.LS",
  fallback: "备用服务",
  "whois-ls": "WHOIS.LS",
  "whois-fallback": "备用服务",
  pending: "等待查询",
  checking: "查询中",
  none: "无",
};

export function providerLabel(name?: string): string {
  if (!name) return "—";
  return PROVIDER_LABELS[name] ?? name;
}

function isEmptyTime(value?: string | null): boolean {
  return !value || value.startsWith("0001-01-01");
}

export function formatDateTime(value?: string | null): string {
  if (isEmptyTime(value)) return "—";
  const date = new Date(value as string);
  if (Number.isNaN(date.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
}

export function formatDate(value?: string | null): string {
  if (isEmptyTime(value)) return "—";
  const date = new Date(value as string);
  if (Number.isNaN(date.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function formatRelative(value?: string | null): string {
  if (isEmptyTime(value)) return "—";
  const date = new Date(value as string);
  if (Number.isNaN(date.getTime())) return "—";

  const diff = date.getTime() - Date.now();
  const abs = Math.abs(diff);
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  const suffix = diff >= 0 ? "后" : "前";
  if (abs < minute) return diff >= 0 ? "即将" : "刚刚";
  if (abs < hour) return `${Math.round(abs / minute)} 分钟${suffix}`;
  if (abs < day) return `${Math.round(abs / hour)} 小时${suffix}`;
  if (abs < 30 * day) return `${Math.round(abs / day)} 天${suffix}`;
  return formatDate(value);
}

export function daysUntil(value?: string | null): number | null {
  if (isEmptyTime(value)) return null;
  const date = new Date(value as string);
  if (Number.isNaN(date.getTime())) return null;
  return Math.ceil((date.getTime() - Date.now()) / 86_400_000);
}

export function formatLatency(ms?: number): string {
  if (ms === undefined || ms === null) return "—";
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export function tldOf(name: string): string {
  const index = name.indexOf(".");
  return index === -1 ? "" : name.slice(index + 1);
}
