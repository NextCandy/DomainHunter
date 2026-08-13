import { useEffect, useMemo, useState } from "react";
import type { BulkAction, BulkPreview } from "../lib/api";
import { api } from "../lib/api";
import { Spinner, useToast } from "./ui";

export function BulkActionPreviewDialog({
  domains,
  open,
  onClose,
  onDone,
}: {
  domains: string[];
  open: boolean;
  onClose: () => void;
  onDone: () => void;
}) {
  const [type, setType] = useState<BulkAction["type"]>("ai_valuation");
  const [tag, setTag] = useState("");
  const [priority, setPriority] = useState("50");
  const [folder, setFolder] = useState("");
  const [notify, setNotify] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [preview, setPreview] = useState<BulkPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [executing, setExecuting] = useState(false);
  const toast = useToast();

  const action = useMemo<BulkAction>(
    () => ({
      type,
      domains,
      ...(type === "tag" ? { tag } : {}),
      ...(type === "priority" ? { priority: Number(priority) } : {}),
      ...(type === "folder"
        ? { folder_id: folder ? Number(folder) : null }
        : {}),
      ...(type === "notification" ? { notify } : {}),
      ...(type === "monitor" ? { enabled } : {}),
    }),
    [domains, enabled, folder, notify, priority, tag, type],
  );

  useEffect(() => {
    if (!open) return;
    setPreview(null);
    setLoading(true);
    if (type === "ai_valuation") {
      setPreview({
        action_type: "ai_valuation",
        matched: domains.length,
        samples: domains.slice(0, 10),
        task_count: domains.length,
        cache_hits: 0,
        warning: "严格估价队列会逐个检查状态与证据；不设置每日额度，已有有效结果会自动命中缓存。",
      });
      setLoading(false);
      return;
    }
    void api.p1
      .bulkPreview(action)
      .then(setPreview)
      .catch((err) =>
        toast(err instanceof Error ? err.message : "预览失败", "error"),
      )
      .finally(() => setLoading(false));
  }, [action, domains, open, toast, type]);

  async function execute() {
    setExecuting(true);
    try {
      if (type === "ai_valuation") {
        const result = await api.domains.batchValuation(domains);
        toast(`已提交 ${result.queued + result.cached} 个 AI 估价任务${result.failed ? `，${result.failed} 个未入队` : ""}`, result.failed ? "info" : "success");
      } else {
        await api.p1.bulkExecute(action);
        toast("批量操作已执行", "success");
      }
      onDone();
      onClose();
    } catch (err) {
      toast(err instanceof Error ? err.message : "批量操作失败", "error");
    } finally {
      setExecuting(false);
    }
  }

  if (!open) return null;
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      role="dialog"
      aria-modal="true"
      aria-label="批量操作预览"
    >
      <button
        type="button"
        className="absolute inset-0 cursor-default bg-overlay/40"
        onClick={onClose}
        aria-label="关闭预览"
      />
      <div className="card relative w-full max-w-lg p-4">
        <header>
          <h2 className="text-[15px] font-semibold">批量操作预览</h2>
          <p className="mt-1 text-[12px] text-ink-muted">
            先确认影响范围，再执行。后端不会为大批量域名发起 N 个 PATCH。
          </p>
        </header>
        <div className="mt-4 grid gap-3 sm:grid-cols-2">
          <label className="text-[12px] text-ink-muted">
            动作
            <select
              className="input mt-1"
              value={type}
              onChange={(event) =>
                setType(event.target.value as BulkAction["type"])
              }
            >
              <option value="ai_valuation">加入 AI 估价</option>
              <option value="tag">添加标签</option>
              <option value="priority">设置优先级</option>
              <option value="folder">移动文件夹</option>
              <option value="notification">通知开关</option>
              <option value="monitor">监控开关</option>
            </select>
          </label>
          {type === "tag" && (
            <label className="text-[12px] text-ink-muted">
              标签
              <input
                className="input mt-1"
                value={tag}
                onChange={(event) => setTag(event.target.value)}
                placeholder="例如 高价值"
              />
            </label>
          )}
          {type === "priority" && (
            <label className="text-[12px] text-ink-muted">
              优先级
              <input
                className="input mt-1"
                type="number"
                min="0"
                max="1000"
                value={priority}
                onChange={(event) => setPriority(event.target.value)}
              />
            </label>
          )}
          {type === "folder" && (
            <label className="text-[12px] text-ink-muted">
              文件夹 ID
              <input
                className="input mt-1"
                type="number"
                min="1"
                value={folder}
                onChange={(event) => setFolder(event.target.value)}
                placeholder="留空移到根目录"
              />
            </label>
          )}
          {type === "notification" && (
            <label className="flex items-center gap-2 self-end text-[12px] text-ink-muted">
              <input
                type="checkbox"
                checked={notify}
                onChange={(event) => setNotify(event.target.checked)}
              />
              启用通知
            </label>
          )}
          {type === "monitor" && (
            <label className="flex items-center gap-2 self-end text-[12px] text-ink-muted">
              <input
                type="checkbox"
                checked={enabled}
                onChange={(event) => setEnabled(event.target.checked)}
              />
              启用监控
            </label>
          )}
        </div>
        <div className="mt-4 rounded-md border border-line bg-surface-muted p-3 text-[12px]">
          {loading ? (
            <div className="flex items-center gap-2 text-ink-muted">
              <Spinner /> 正在计算影响范围…
            </div>
          ) : preview ? (
            <div className="grid grid-cols-2 gap-2">
              <div>
                实际匹配 <strong>{preview.matched}</strong> 个
              </div>
              <div>
                将创建任务 <strong>{preview.task_count}</strong> 个
              </div>
              {type === "ai_valuation" && (
                <>
                  <div>
                    缓存命中 <strong>{preview.cache_hits}</strong> 个
                  </div>
                  <div>
                    预估 tokens <strong>{preview.task_count * 900}</strong>
                  </div>
                  <div>
                    每日额度 <strong>不限</strong>
                  </div>
                </>
              )}
              <div className="col-span-2 text-ink-faint">
                样例：
                {preview.samples.length ? preview.samples.join("、") : "无"}
              </div>
              {preview.warning && (
                <p className="col-span-2 text-ink-muted">{preview.warning}</p>
              )}
            </div>
          ) : null}
        </div>
        <footer className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            disabled={!preview || loading || executing}
            onClick={() => void execute()}
          >
            {executing && <Spinner />}
            {type === "ai_valuation" ? "确认入队" : "确认执行"}
          </button>
        </footer>
      </div>
    </div>
  );
}
