import { useEffect, useState } from "react";
import type { FilterNode, SavedView } from "../lib/api";
import { api } from "../lib/api";
import { ErrorNotice, Spinner, useToast } from "./ui";

export function SavedViewMenu({
  filter,
  onApply,
  onUnauthorized,
}: {
  filter: FilterNode;
  onApply: (filter: FilterNode) => void;
  onUnauthorized: () => void;
}) {
  const [views, setViews] = useState<SavedView[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  useEffect(() => {
    let active = true;
    void api.p1.savedViews.list().then((result) => { if (active) setViews(result.views ?? []); }).catch((err) => {
      if (err?.status === 401) onUnauthorized(); else if (active) setError(err instanceof Error ? err.message : "保存视图加载失败");
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [onUnauthorized]);

  async function save() {
    if (!name.trim()) return;
    try {
      const created = await api.p1.savedViews.create({ name: name.trim(), filter, shared: false });
      setViews((current) => [created, ...current]);
      setName("");
      toast("智能视图已保存", "success");
    } catch (err) { toast(err instanceof Error ? err.message : "保存视图失败", "error"); }
  }

  async function remove(view: SavedView) {
    if (!window.confirm(`删除视图“${view.name}”？`)) return;
    try { await api.p1.savedViews.remove(view.id); setViews((current) => current.filter((item) => item.id !== view.id)); toast("视图已删除", "success"); }
    catch (err) { toast(err instanceof Error ? err.message : "删除视图失败", "error"); }
  }

  return (
    <div className="relative">
      <button type="button" className="btn h-8" onClick={() => setOpen((value) => !value)} aria-expanded={open}>智能视图</button>
      {open && (
        <div className="absolute right-0 top-10 z-20 w-80 rounded-card border border-line bg-surface-raised p-3">
          <div className="flex gap-2">
            <input className="input h-8 min-w-0 flex-1 text-[12px]" value={name} onChange={(event) => setName(event.target.value)} placeholder="新视图名称" onKeyDown={(event) => { if (event.key === "Enter") void save(); }} />
            <button type="button" className="btn btn-primary h-8 px-2 text-[12px]" onClick={() => void save()} disabled={!name.trim()}>保存</button>
          </div>
          <div className="my-3 border-t border-line" />
          {loading ? <div className="flex items-center gap-2 py-3 text-[12px] text-ink-muted"><Spinner /> 加载中…</div> : error ? <ErrorNotice message={error} /> : views.length === 0 ? <p className="py-3 text-[12px] text-ink-faint">还没有保存的视图</p> : (
            <div className="max-h-64 space-y-1 overflow-y-auto">
              {views.map((view) => <div key={view.id} className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-surface-muted"><button type="button" className="min-w-0 flex-1 truncate text-left text-[12px] text-ink" onClick={() => { onApply(view.filter); setOpen(false); }}>{view.name}</button><button type="button" className="text-[11px] text-ink-faint hover:text-ink" onClick={() => void remove(view)} aria-label={`删除${view.name}`}>删除</button></div>)}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
