import type { AIUsage } from "../lib/api";

export function AIUsageMeter({ usage }: { usage: AIUsage | null }) {
  if (!usage) return null;
  return (
    <section className="card p-4">
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-[14px] font-semibold">AI 用量 · 不限每日额度</h2>
        <span className="text-[12px] text-ink-muted">{usage.date}</span>
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2 text-[12px] text-ink-muted sm:grid-cols-5">
        <span>今日已处理 {usage.used}</span>
        <span>排队 {usage.queued}</span>
        <span>运行 {usage.running}</span>
        <span>成功 {usage.succeeded}</span>
        <span>失败 {usage.failed}</span>
      </div>
    </section>
  );
}
