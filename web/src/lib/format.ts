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

/** 状态色板：语义清晰、在深浅两套主题下对比度都足够 */
export const STATUS_CLASSES: Record<DomainStatus, string> = {
  available:
    "bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:border-emerald-500/25",
  registered:
    "bg-zinc-100 text-zinc-700 border-zinc-200 dark:bg-zinc-500/10 dark:text-zinc-300 dark:border-zinc-500/25",
  grace:
    "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:border-amber-500/25",
  redemption:
    "bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:border-orange-500/25",
  pending_delete:
    "bg-rose-50 text-rose-700 border-rose-200 dark:bg-rose-500/10 dark:text-rose-300 dark:border-rose-500/25",
  expired:
    "bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:border-orange-500/25",
  transfer_locked:
    "bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-500/10 dark:text-sky-300 dark:border-sky-500/25",
  hold: "bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-500/10 dark:text-sky-300 dark:border-sky-500/25",
  unknown:
    "bg-zinc-100 text-zinc-600 border-zinc-200 dark:bg-zinc-500/10 dark:text-zinc-400 dark:border-zinc-500/25",
  error:
    "bg-red-50 text-red-700 border-red-200 dark:bg-red-500/10 dark:text-red-300 dark:border-red-500/25",
  skipped:
    "bg-slate-100 text-slate-600 border-slate-200 dark:bg-slate-500/10 dark:text-slate-400 dark:border-slate-500/25",
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
