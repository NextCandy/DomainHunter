import { useEffect, useState } from "react";
import type { FilterNode } from "../lib/api";
import { cx } from "./ui";

const STATUS_OPTIONS = [
  ["available", "可注册"],
  ["registered", "已注册"],
  ["grace", "宽限期"],
  ["redemption", "赎回期"],
  ["pending_delete", "待删除"],
  ["expired", "已过期"],
  ["unknown", "未知"],
  ["error", "查询异常"],
] as const;

export function AdvancedFilterDrawer({
  open,
  initial,
  onApply,
  onClose,
}: {
  open: boolean;
  initial: FilterNode;
  onApply: (filter: FilterNode) => void;
  onClose: () => void;
}) {
  const [status, setStatus] = useState("");
  const [tld, setTld] = useState("");
  const [registrar, setRegistrar] = useState("");
  const [tag, setTag] = useState("");
  const [quality, setQuality] = useState("");
  const [logic, setLogic] = useState<"and" | "or">("and");

  useEffect(() => {
    const conditions = initial.conditions ?? [];
    const read = (field: string) => conditions.find((item) => item.field === field);
    const statusItem = read("status");
    const qualityItem = read("ai_quality");
    setLogic(initial.logic ?? "and");
    setStatus(typeof statusItem?.value === "string" ? statusItem.value : "");
    setTld(typeof read("tld")?.value === "string" ? String(read("tld")?.value) : "");
    setRegistrar(typeof read("registrar")?.value === "string" ? String(read("registrar")?.value) : "");
    setTag(typeof read("tag")?.value === "string" ? String(read("tag")?.value) : "");
    setQuality(typeof qualityItem?.value === "number" || typeof qualityItem?.value === "string" ? String(qualityItem.value) : "");
  }, [initial, open]);

  if (!open) return null;

  function apply() {
    const conditions: FilterNode[] = [];
    if (status) conditions.push({ field: "status", op: "eq", value: status });
    if (tld.trim()) conditions.push({ field: "tld", op: "eq", value: tld.trim().replace(/^\./, "") });
    if (registrar.trim()) conditions.push({ field: "registrar", op: "contains", value: registrar.trim() });
    if (tag.trim()) conditions.push({ field: "tag", op: "contains", value: tag.trim() });
    if (quality.trim() && Number.isFinite(Number(quality))) {
      conditions.push({ field: "ai_quality", op: "gte", value: Number(quality) });
    }
    onApply({ version: 1, logic, conditions });
    onClose();
  }

  return (
    <div className="fixed inset-0 z-50 flex justify-end" role="dialog" aria-modal="true" aria-label="高级筛选">
      <button type="button" className="absolute inset-0 cursor-default bg-black/30" aria-label="关闭高级筛选" onClick={onClose} />
      <aside className="relative flex h-full w-full max-w-md flex-col border-l border-line bg-surface-raised p-4 shadow-xl">
        <header className="flex items-start justify-between gap-3 border-b border-line pb-3">
          <div>
            <h2 className="text-[15px] font-semibold">高级筛选</h2>
            <p className="mt-1 text-[12px] text-ink-muted">条件会编码到 URL，可刷新、分享，不携带私密数据。</p>
          </div>
          <button type="button" className="btn h-7 px-2" onClick={onClose} aria-label="关闭">
            ×
          </button>
        </header>
        <div className="flex-1 space-y-4 overflow-y-auto py-4">
          <label className="block text-[12px] text-ink-muted">
            条件关系
            <select className="input mt-1" value={logic} onChange={(event) => setLogic(event.target.value as "and" | "or")}>
              <option value="and">同时满足（AND）</option>
              <option value="or">满足任一（OR）</option>
            </select>
          </label>
          <label className="block text-[12px] text-ink-muted">
            状态
            <select className="input mt-1" value={status} onChange={(event) => setStatus(event.target.value)}>
              <option value="">不限</option>
              {STATUS_OPTIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
          <label className="block text-[12px] text-ink-muted">
            后缀
            <input className="input mt-1" value={tld} onChange={(event) => setTld(event.target.value)} placeholder="例如 im 或 .cn" />
          </label>
          <label className="block text-[12px] text-ink-muted">
            注册商包含
            <input className="input mt-1" value={registrar} onChange={(event) => setRegistrar(event.target.value)} placeholder="注册商名称" />
          </label>
          <label className="block text-[12px] text-ink-muted">
            标签包含
            <input className="input mt-1" value={tag} onChange={(event) => setTag(event.target.value)} placeholder="标签" />
          </label>
          <label className="block text-[12px] text-ink-muted">
            AI 质量分不低于
            <input className="input mt-1" type="number" min="0" max="100" value={quality} onChange={(event) => setQuality(event.target.value)} placeholder="例如 70" />
          </label>
          <p className={cx("rounded-md border border-line bg-surface-muted px-3 py-2 text-[11px] text-ink-faint")}>AI 条件只读取已持久化估价，不会触发模型请求。</p>
        </div>
        <footer className="flex justify-end gap-2 border-t border-line pt-3">
          <button type="button" className="btn" onClick={() => { setStatus(""); setTld(""); setRegistrar(""); setTag(""); setQuality(""); }}>清空</button>
          <button type="button" className="btn btn-primary" onClick={apply}>应用筛选</button>
        </footer>
      </aside>
    </div>
  );
}
