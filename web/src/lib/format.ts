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

/** 状态颜色只引用语义令牌；浅色/深色主题由 CSS 统一校准。 */
export const STATUS_CLASSES: Record<DomainStatus, string> = {
  available: "bg-success/12 text-success border-success/20",
  registered: "bg-neutral/12 text-neutral border-neutral/20",
  grace: "bg-warning/12 text-warning border-warning/20",
  redemption: "bg-info/12 text-info border-info/20",
  pending_delete: "bg-danger/12 text-danger border-danger/20",
  expired: "bg-danger/12 text-danger border-danger/20",
  transfer_locked: "bg-info/12 text-info border-info/20",
  hold: "bg-info/12 text-info border-info/20",
  unknown: "bg-neutral/12 text-neutral border-neutral/20",
  error: "bg-danger/12 text-danger border-danger/20",
  skipped: "bg-neutral/12 text-neutral border-neutral/20",
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

export interface ReleaseWindow {
  label: string;
  earliest: Date;
  latest: Date;
  daysRemaining: number;
}

/** 基于公开生命周期阶段的研究性时间窗，不作为注册局承诺。 */
export function predictReleaseWindow(name: string, expiry?: string | null): ReleaseWindow | null {
  if (!expiry) return null;
  const start = new Date(expiry);
  if (Number.isNaN(start.getTime())) return null;
  const tld = tldOf(name);
  const center = tld === "cn" ? 65 : tld === "org" ? 77 : 75;
  const earliest = new Date(start);
  const latest = new Date(start);
  earliest.setDate(earliest.getDate() + center - 3);
  latest.setDate(latest.getDate() + center + 3);
  return {
    label: `预计 ${formatDate(earliest.toISOString())} — ${formatDate(latest.toISOString())}`,
    earliest,
    latest,
    daysRemaining: Math.ceil((earliest.getTime() - Date.now()) / 86_400_000),
  };
}
