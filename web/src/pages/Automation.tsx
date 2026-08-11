import { useState } from "react";
import { api } from "../lib/api";
import type { AutomationRuleInput } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { AIJobList } from "../components/AIJobList";
import { AIProviderSettingsForm } from "../components/AIProviderSettingsForm";
import { AIUsageMeter } from "../components/AIUsageMeter";
import { ErrorNotice, Spinner, useToast } from "../components/ui";

const TRIGGERS = [
  ["domain_added", "新增域名"],
  ["observation_completed", "观测完成"],
  ["status_changed", "确认状态变化"],
  ["error", "查询异常"],
  ["recovery", "异常恢复"],
  ["expiry_scan", "临近到期扫描"],
] as const;

export function AutomationPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const settings = useAsync(() => api.p1.ai.settings(), [], onUnauthorized);
  const models = useAsync(() => api.p1.ai.models(), [], onUnauthorized);
  const usage = useAsync(() => api.p1.ai.usage(), [], onUnauthorized);
  const jobs = useAsync(() => api.p1.ai.jobs(), [], onUnauthorized);
  const rules = useAsync(() => api.p1.automation.rules(), [], onUnauthorized);
  const runs = useAsync(() => api.p1.automation.runs(), [], onUnauthorized);
  const toast = useToast();
  const [ruleName, setRuleName] = useState("");
  const [trigger, setTrigger] = useState("status_changed");
  const [conditionStatus, setConditionStatus] = useState("available");
  const [actionType, setActionType] = useState("tag");
  const [actionTag, setActionTag] = useState("高价值");
  const [ruleDryRun, setRuleDryRun] = useState(true);
  const [saving, setSaving] = useState(false);

  async function reloadAll() { await Promise.all([settings.reload(), usage.reload(), jobs.reload(), rules.reload(), runs.reload()]); }

  async function createRule() {
    if (!ruleName.trim()) return;
    const rule: AutomationRuleInput = {
      name: ruleName.trim(), enabled: true, dry_run: ruleDryRun,
      trigger: { version: 1, field: "event_type", op: "eq", value: trigger },
      conditions: { version: 1, logic: "and", conditions: [{ field: "status", op: "eq", value: conditionStatus }] },
      actions: actionType === "ai_valuation" ? [{ type: "ai_valuation" }] : [{ type: "tag", tag: actionTag.trim() }],
      cooldown_seconds: 86400, daily_run_cap: 100,
    };
    setSaving(true);
    try { await api.p1.automation.createRule(rule); setRuleName(""); toast("自动化规则已创建", "success"); await reloadAll(); }
    catch (err) { toast(err instanceof Error ? err.message : "规则创建失败", "error"); }
    finally { setSaving(false); }
  }

  async function dryRun(id: number) {
    try { const result = await api.p1.automation.dryRun(id, { event_id: `manual-${Date.now()}`, type: trigger }); toast(`Dry-run 匹配 ${result.runs.length} 个域名；未修改数据`, "success"); await runs.reload(); }
    catch (err) { toast(err instanceof Error ? err.message : "Dry-run 失败", "error"); }
  }

  return <div className="space-y-5"><header><h1 className="text-[18px] font-semibold tracking-tight">自动化与 AI 研究</h1><p className="mt-1 text-[12px] text-ink-muted">触发器 → 条件 → 安全动作 → 防护栏。默认 Dry-run，不允许购买、删除、改密钥或任意 URL。</p></header>{settings.error && <ErrorNotice message={settings.error} onRetry={reloadAll} />}<AIProviderSettingsForm settings={settings.data ?? null} models={models.data?.models ?? []} onSaved={reloadAll} /><AIUsageMeter usage={usage.data ?? null} /><AIJobList jobs={jobs.data?.jobs ?? []} /><section className="card p-4"><div className="flex flex-wrap items-start justify-between gap-2"><div><h2 className="text-[14px] font-semibold">规则构建器</h2><p className="mt-1 text-[12px] text-ink-muted">用表单生成安全规则，服务端会再次校验动作白名单。</p></div></div><div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4"><label className="text-[12px] text-ink-muted">规则名称<input className="input mt-1" value={ruleName} onChange={(event) => setRuleName(event.target.value)} placeholder="可注册域名加标签" /></label><label className="text-[12px] text-ink-muted">触发器<select className="input mt-1" value={trigger} onChange={(event) => setTrigger(event.target.value)}>{TRIGGERS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label className="text-[12px] text-ink-muted">条件：状态<select className="input mt-1" value={conditionStatus} onChange={(event) => setConditionStatus(event.target.value)}><option value="available">可注册</option><option value="registered">已注册</option><option value="error">查询异常</option><option value="unknown">未知</option></select></label><label className="text-[12px] text-ink-muted">动作<select className="input mt-1" value={actionType} onChange={(event) => setActionType(event.target.value)}><option value="tag">添加标签</option><option value="ai_valuation">加入 AI 估价队列</option></select></label></div>{actionType === "tag" && <label className="mt-3 block max-w-sm text-[12px] text-ink-muted">标签<input className="input mt-1" value={actionTag} onChange={(event) => setActionTag(event.target.value)} /></label>}<label className="mt-3 flex items-center gap-2 text-[12px] text-ink-muted"><input type="checkbox" checked={ruleDryRun} onChange={(event) => setRuleDryRun(event.target.checked)} />创建为 Dry-run（推荐）</label><div className="mt-4 flex justify-end"><button type="button" className="btn btn-primary" disabled={saving || !ruleName.trim()} onClick={() => void createRule()}>{saving && <Spinner />}创建规则</button></div></section><section className="card p-4"><div className="flex items-center justify-between gap-2"><h2 className="text-[14px] font-semibold">规则与运行审计</h2><button type="button" className="btn h-7 px-2 text-[12px]" onClick={() => void reloadAll()}>刷新</button></div>{(rules.data?.rules ?? []).length === 0 ? <p className="mt-3 text-[12px] text-ink-faint">暂无规则</p> : <div className="mt-3 space-y-2">{rules.data?.rules.map((rule) => <div key={rule.id} className="rounded-md border border-line p-3"><div className="flex flex-wrap items-center gap-2"><strong className="text-[13px]">{rule.name}</strong><span className="rounded-full border border-line px-2 py-0.5 text-[11px] text-ink-muted">{rule.enabled ? "启用" : "停用"}</span><span className="rounded-full border border-line px-2 py-0.5 text-[11px] text-ink-muted">{rule.dry_run ? "Dry-run" : "执行"}</span><span className="text-[11px] text-ink-faint">每日 {rule.daily_run_cap} 次 · 冷却 {rule.cooldown_seconds}s</span><button type="button" className="btn ml-auto h-7 px-2 text-[11px]" onClick={() => void dryRun(rule.id)}>Dry-run</button></div><p className="mt-2 text-[11px] text-ink-muted">触发 {String(rule.trigger.value ?? "事件")} · 条件状态 {String(rule.conditions.conditions?.[0]?.value ?? "不限")} · 动作 {String(rule.actions[0]?.type ?? "—")}</p></div>)}</div>}<div className="mt-5 border-t border-line pt-4"><h3 className="text-[12px] font-medium text-ink-muted">最近运行</h3>{(runs.data?.runs ?? []).length === 0 ? <p className="mt-2 text-[12px] text-ink-faint">暂无审计记录</p> : <div className="table-scroll mt-2"><table className="w-full min-w-[620px] text-left text-[12px]"><thead className="border-b border-line text-ink-muted"><tr><th className="px-2 py-2 font-medium">规则</th><th className="px-2 py-2 font-medium">域名</th><th className="px-2 py-2 font-medium">状态</th><th className="px-2 py-2 font-medium">事件</th><th className="px-2 py-2 font-medium">动作数</th></tr></thead><tbody className="divide-y divide-line">{runs.data?.runs.map((run) => <tr key={run.id}><td className="px-2 py-2">#{run.rule_id}</td><td className="mono px-2 py-2">{run.domain || "—"}</td><td className="px-2 py-2">{run.status}</td><td className="mono px-2 py-2 text-ink-muted">{run.event_id}</td><td className="tabular px-2 py-2">{run.action_count}</td></tr>)}</tbody></table></div>}</div></section></div>;
}
