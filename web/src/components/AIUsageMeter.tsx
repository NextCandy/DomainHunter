import type { AIUsage } from "../lib/api";

export function AIUsageMeter({ usage }: { usage: AIUsage | null }) {
  if (!usage) return null;
  const ratio = Math.min(100, usage.daily_limit ? (usage.used / usage.daily_limit) * 100 : 0);
  return <section className="card p-4"><div className="flex items-center justify-between gap-2"><h2 className="text-[14px] font-semibold">AI 用量</h2><span className="text-[12px] text-ink-muted">{usage.date}</span></div><div className="mt-3 h-2 overflow-hidden rounded-full bg-surface-muted"><div className="h-full rounded-full bg-accent transition-all" style={{ width: `${ratio}%` }} /></div><div className="mt-2 grid grid-cols-2 gap-2 text-[12px] text-ink-muted sm:grid-cols-5"><span>已用 {usage.used}/{usage.daily_limit}</span><span>排队 {usage.queued}</span><span>运行 {usage.running}</span><span>成功 {usage.succeeded}</span><span>失败 {usage.failed}</span></div></section>;
}
