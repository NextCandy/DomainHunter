import { useEffect, useState } from "react";
import type { Valuation } from "../lib/api";
import { api } from "../lib/api";
import { Spinner, useToast } from "./ui";

export function AIDomainValuationCard({ domain }: { domain: string }) {
  const [valuation, setValuation] = useState<Valuation | null>(null);
  const [loading, setLoading] = useState(true);
  const [queued, setQueued] = useState(false);
  const toast = useToast();

  useEffect(() => {
    let active = true;
    setLoading(true);
    void api.p1.ai.valuation(domain).then((result) => { if (active) setValuation(result.valuation); }).catch(() => { if (active) setValuation(null); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [domain]);

  async function enqueue() {
    setQueued(true);
    try { await api.p1.ai.enqueue([domain]); toast("AI 研究任务已入队", "success"); }
    catch (err) { toast(err instanceof Error ? err.message : "AI 任务入队失败", "error"); }
    finally { setQueued(false); }
  }

  return (
    <section className="mt-4 rounded-md border border-accent/25 bg-accent-soft/30 p-3">
      <div className="flex items-center justify-between gap-2">
        <div><h3 className="text-[12px] font-medium">AI 研究性估价</h3><p className="mt-1 text-[11px] text-ink-faint">仅辅助排序与解释，不改变可注册结论。</p></div>
        {!valuation && <button type="button" className="btn h-7 px-2 text-[11px]" onClick={() => void enqueue()} disabled={queued}>{queued && <Spinner />}加入队列</button>}
      </div>
      {loading ? <div className="mt-3 flex items-center gap-2 text-[11px] text-ink-muted"><Spinner />读取已持久化结果…</div> : valuation ? <ValuationBreakdown valuation={valuation} /> : <p className="mt-3 text-[11px] text-ink-muted">暂无估价结果。入队后由低并发 Worker 处理。</p>}
    </section>
  );
}

export function ValuationBreakdown({ valuation }: { valuation: Valuation }) {
  return (
    <div className="mt-3 space-y-2 text-[11px]">
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4"><Metric label="质量" value={`${valuation.quality_score}/100`} /><Metric label="流动性" value={`${valuation.liquidity_score}/100`} /><Metric label="风险" value={valuation.risk_level} /><Metric label="置信度" value={valuation.confidence} /></div>
      <div className="rounded border border-line bg-surface-raised px-2 py-1.5">研究区间：<strong>¥{valuation.value_low.toLocaleString()} – ¥{valuation.value_high.toLocaleString()}</strong></div>
      <p className="text-ink-muted">{valuation.rationale}</p>
      {valuation.data_gaps.length > 0 && <p className="text-amber-700 dark:text-amber-300">数据不足：{valuation.data_gaps.join("、")}</p>}
      <p className="text-ink-faint">{valuation.disclaimer}</p>
      <div className="text-ink-faint">模型 {valuation.model} · 生成于 {new Date(valuation.generated_at).toLocaleString()}</div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) { return <div className="rounded border border-line bg-surface-raised px-2 py-1"><span className="text-ink-faint">{label}</span><strong className="ml-1">{value}</strong></div>; }
