import { useCallback, useEffect, useState } from "react";
import { ApiError, api } from "../lib/api";
import type { FilterNode, SavedView } from "../lib/api";
import { ErrorNotice, Spinner, useToast } from "./ui";

export function SavedViewMenu({
  filter,
  onApply,
  onUnauthorized,
  embedded = false,
}: {
  filter: FilterNode;
  onApply: (filter: FilterNode) => void;
  onUnauthorized: () => void;
  embedded?: boolean;
}) {
  const [views, setViews] = useState<SavedView[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const toast = useToast();

  const loadViews = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await api.p1.savedViews.list();
      setViews(result.views ?? []);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      else setError(err instanceof Error ? err.message : "保存视图加载失败");
    } finally {
      setLoading(false);
    }
  }, [onUnauthorized]);

  useEffect(() => {
    void loadViews();
  }, [loadViews]);

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

  async function overwrite(view: SavedView) {
    if (!window.confirm(`用当前筛选覆盖视图“${view.name}”？`)) return;
    try {
      const updated = await api.p1.savedViews.update(view.id, { name: view.name, filter, shared: view.shared });
      setViews((current) => current.map((item) => item.id === view.id ? updated : item));
      toast("视图筛选已更新", "success");
    } catch (err) { toast(err instanceof Error ? err.message : "更新视图失败", "error"); }
  }

  async function rename(view: SavedView) {
    const nextName = window.prompt("新的视图名称", view.name)?.trim();
    if (!nextName || nextName === view.name) return;
    try {
      const updated = await api.p1.savedViews.update(view.id, { name: nextName, filter: view.filter, shared: view.shared });
      setViews((current) => current.map((item) => item.id === view.id ? updated : item));
      toast("视图已重命名", "success");
    } catch (err) { toast(err instanceof Error ? err.message : "重命名视图失败", "error"); }
  }

  return (
    <div className="relative">
      <button type="button" className={embedded ? "min-h-11 w-full rounded-md px-3 text-left text-[12px] hover:bg-surface-muted" : "btn h-8"} onClick={() => setOpen((value) => !value)} aria-expanded={open}>智能视图</button>
      {open && (
        <div className={embedded ? "mt-1 w-full rounded-card border border-line bg-surface p-3" : "absolute right-0 top-10 z-20 w-80 rounded-card border border-line bg-surface-raised p-3"}>
          <div className="flex gap-2">
            <input className="input h-8 min-w-0 flex-1 text-[12px]" value={name} onChange={(event) => setName(event.target.value)} placeholder="新视图名称" onKeyDown={(event) => { if (event.key === "Enter") void save(); }} />
            <button type="button" className="btn btn-primary h-8 px-2 text-[12px]" onClick={() => void save()} disabled={!name.trim()}>保存</button>
          </div>
          <div className="my-3 border-t border-line" />
          {loading ? <div className="flex items-center gap-2 py-3 text-[12px] text-ink-muted"><Spinner /> 加载中…</div> : error ? <ErrorNotice message={error} onRetry={() => void loadViews()} /> : views.length === 0 ? <p className="py-3 text-[12px] text-ink-faint">还没有保存的视图</p> : (
            <div className="max-h-64 space-y-1 overflow-y-auto">
              {views.map((view) => <div key={view.id} className="flex flex-wrap items-center gap-1 rounded-md px-2 py-1.5 hover:bg-surface-muted"><button type="button" className="min-h-9 min-w-0 flex-1 truncate text-left text-[12px] text-ink" onClick={() => { onApply(view.filter); setOpen(false); }}>{view.name}</button><button type="button" className="min-h-9 px-1 text-[11px] text-ink-faint hover:text-ink" onClick={() => void overwrite(view)} aria-label={`覆盖${view.name}`}>覆盖</button><button type="button" className="min-h-9 px-1 text-[11px] text-ink-faint hover:text-ink" onClick={() => void rename(view)} aria-label={`重命名${view.name}`}>改名</button><button type="button" className="min-h-9 px-1 text-[11px] text-danger hover:text-danger" onClick={() => void remove(view)} aria-label={`删除${view.name}`}>删除</button></div>)}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
