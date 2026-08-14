import { useCallback, useMemo, useState } from "react";
import { api } from "../lib/api";
import type { FolderTreeNode } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { ConfirmDialog, ErrorNotice, Spinner, cx, useToast } from "./ui";

export type FolderSelection = "all" | "root" | number;

interface FolderTreeProps {
  selected: FolderSelection;
  onSelect: (selection: FolderSelection) => void;
  onDomainDrop: (folderId: number | null, domains: string[]) => Promise<void>;
  onUnauthorized: () => void;
}

export function FolderTree({ selected, onSelect, onDomainDrop, onUnauthorized }: FolderTreeProps) {
  const toast = useToast();
  const folders = useAsync(() => api.folders.list(), [], onUnauthorized);
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [dialog, setDialog] = useState<{ folder: FolderTreeNode | null; parentId: number | null } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<FolderTreeNode | null>(null);
  const [busy, setBusy] = useState(false);

  const tree = useMemo(() => folders.data?.tree ?? [], [folders.data]);

  const reload = useCallback(() => folders.reload(), [folders]);

  async function saveFolder(name: string, parentId: number | null, folder: FolderTreeNode | null) {
    setBusy(true);
    try {
      if (folder) {
        await api.folders.update(folder.id, name, parentId);
        toast("文件夹已更新", "success");
      } else {
        await api.folders.create(name, parentId);
        toast("文件夹已创建", "success");
      }
      setDialog(null);
      reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "保存文件夹失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function deleteFolder() {
    if (!pendingDelete) return;
    setBusy(true);
    try {
      await api.folders.remove(pendingDelete.id);
      if (selected === pendingDelete.id) onSelect("root");
      toast("文件夹已删除，域名已移回根目录", "success");
      setPendingDelete(null);
      reload();
    } catch (error) {
      toast(error instanceof Error ? error.message : "删除文件夹失败", "error");
    } finally {
      setBusy(false);
    }
  }

  async function handleDrop(event: React.DragEvent, folderId: number | null) {
    event.preventDefault();
    event.stopPropagation();
    const raw =
      event.dataTransfer.getData("application/x-domainhunter-domains") ||
      event.dataTransfer.getData("text/plain");
    if (!raw) return;
    let domains: string[];
    try {
      const parsed = JSON.parse(raw);
      domains = Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string") : [raw];
    } catch {
      domains = [raw];
    }
    const uniqueDomains = Array.from(new Set(domains.map((item) => item.trim()).filter(Boolean)));
    if (uniqueDomains.length === 0) return;
    await onDomainDrop(folderId, uniqueDomains);
  }

  function toggleExpanded(id: number) {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <section className="card h-fit" aria-label="域名文件夹">
      <header className="flex items-center justify-between gap-2 border-b border-line px-3 py-2.5">
        <div>
          <h2 className="text-[13px] font-semibold">文件夹</h2>
          <p className="text-[11px] text-ink-faint">拖动域名到文件夹即可移动</p>
        </div>
        <button
          type="button"
          className="btn btn-ghost h-7 w-7 px-0"
          onClick={() => setDialog({ folder: null, parentId: null })}
          aria-label="创建根文件夹"
          title="创建根文件夹"
        >
          <span aria-hidden="true" className="text-base leading-none">＋</span>
        </button>
      </header>

      <div className="p-2">
        {folders.error && <ErrorNotice message={folders.error} onRetry={folders.reload} />}
        {folders.loading && !folders.data ? (
          <div className="flex items-center gap-2 px-2 py-5 text-[12px] text-ink-muted">
            <Spinner /> 加载文件夹…
          </div>
        ) : (
          <ul
            role="tree"
            aria-label="文件夹树"
            className="space-y-0.5"
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => void handleDrop(event, null)}
          >
            <li
              role="treeitem"
              aria-selected={selected === "all"}
            >
              <button
                type="button"
              className={cx(
                "flex w-full items-center rounded-md px-2 py-1.5 text-left text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
                selected === "all" ? "bg-surface-muted font-medium text-ink" : "text-ink-muted hover:bg-surface-muted",
              )}
              onClick={() => onSelect("all")}
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => void handleDrop(event, null)}
              >
              <span className="mr-2 w-4 text-center" aria-hidden="true">⌁</span>
              全部域名
              </button>
            </li>
            <li
              role="treeitem"
              aria-selected={selected === "root"}
            >
              <button
                type="button"
              className={cx(
                "mt-0.5 flex w-full items-center rounded-md px-2 py-1.5 text-left text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
                selected === "root" ? "bg-surface-muted font-medium text-ink" : "text-ink-muted hover:bg-surface-muted",
              )}
              onClick={() => onSelect("root")}
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => void handleDrop(event, null)}
              >
              <span className="mr-2 w-4 text-center" aria-hidden="true">⌂</span>
              未归档
              </button>
            </li>
            {tree.map((node) => (
              <FolderNode
                key={node.id}
                node={node}
                depth={0}
                expanded={expanded}
                selected={selected}
                onSelect={onSelect}
                onToggle={toggleExpanded}
                onCreateChild={(parentId) => setDialog({ folder: null, parentId })}
                onRename={(folder) => setDialog({ folder, parentId: folder.parent_id ?? null })}
                onDelete={setPendingDelete}
                onDrop={handleDrop}
              />
            ))}
          </ul>
        )}
      </div>

      {dialog && (
        <FolderFormDialog
          folder={dialog.folder}
          parentId={dialog.parentId}
          busy={busy}
          onCancel={() => setDialog(null)}
          onSave={saveFolder}
        />
      )}
      <ConfirmDialog
        open={Boolean(pendingDelete)}
        title="删除文件夹"
        description={pendingDelete ? `删除“${pendingDelete.name}”后，其中的域名会移回未归档，子文件夹也会移到根目录。` : ""}
        confirmText="删除文件夹"
        danger
        onConfirm={() => void deleteFolder()}
        onCancel={() => setPendingDelete(null)}
      />
    </section>
  );
}

interface FolderNodeProps {
  node: FolderTreeNode;
  depth: number;
  expanded: Set<number>;
  selected: FolderSelection;
  onSelect: (selection: FolderSelection) => void;
  onToggle: (id: number) => void;
  onCreateChild: (parentId: number) => void;
  onRename: (folder: FolderTreeNode) => void;
  onDelete: (folder: FolderTreeNode) => void;
  onDrop: (event: React.DragEvent, folderId: number | null) => Promise<void>;
}

function FolderNode({
  node,
  depth,
  expanded,
  selected,
  onSelect,
  onToggle,
  onCreateChild,
  onRename,
  onDelete,
  onDrop,
}: FolderNodeProps) {
  const children = node.children ?? [];
  const isExpanded = expanded.has(node.id);
  return (
    <li
      role="treeitem"
      aria-level={depth + 1}
      aria-expanded={children.length > 0 ? isExpanded : undefined}
      aria-selected={selected === node.id}
    >
      <div
        className="group flex items-center gap-0.5 rounded-md hover:bg-surface-muted"
        style={{ paddingLeft: `${depth * 0.75}rem` }}
        onDragOver={(event) => event.preventDefault()}
        onDrop={(event) => void onDrop(event, node.id)}
      >
        {children.length > 0 ? (
          <button
            type="button"
            className="btn btn-ghost h-7 w-6 shrink-0 px-0 text-ink-faint"
            onClick={() => onToggle(node.id)}
            aria-label={isExpanded ? `收起 ${node.name}` : `展开 ${node.name}`}
            title={isExpanded ? "收起子文件夹" : "展开子文件夹"}
          >
            <span aria-hidden="true">{isExpanded ? "▾" : "▸"}</span>
          </button>
        ) : (
          <span className="w-6 shrink-0" aria-hidden="true" />
        )}
        <button
          type="button"
          className={cx(
            "min-w-0 flex-1 truncate rounded-md px-1.5 py-1.5 text-left text-[13px] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent",
            selected === node.id ? "font-medium text-ink" : "text-ink-muted",
          )}
          onClick={() => onSelect(node.id)}
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => void onDrop(event, node.id)}
          title={`选择文件夹 ${node.name}`}
        >
          <span className="mr-1.5 text-ink-faint" aria-hidden="true">▱</span>
          {node.name}
        </button>
        <button
          type="button"
          className="btn btn-ghost h-7 w-7 shrink-0 px-0 text-ink-faint opacity-70 focus-visible:opacity-100 group-hover:opacity-100"
          onClick={() => onCreateChild(node.id)}
          aria-label={`在 ${node.name} 下创建文件夹`}
          title={`在 ${node.name} 下创建文件夹`}
        >
          <span aria-hidden="true">＋</span>
        </button>
        <button
          type="button"
          className="btn btn-ghost h-7 w-7 shrink-0 px-0 text-ink-faint opacity-70 focus-visible:opacity-100 group-hover:opacity-100"
          onClick={() => onRename(node)}
          aria-label={`重命名 ${node.name}`}
          title={`重命名 ${node.name}`}
        >
          <span aria-hidden="true">✎</span>
        </button>
        <button
          type="button"
          className="btn btn-ghost h-7 w-7 shrink-0 px-0 text-ink-muted opacity-70 focus-visible:opacity-100 group-hover:opacity-100"
          onClick={() => onDelete(node)}
          aria-label={`删除 ${node.name}`}
          title={`删除 ${node.name}`}
        >
          <span aria-hidden="true">×</span>
        </button>
      </div>
      {isExpanded && children.length > 0 && (
        <ul className="space-y-0.5" role="group">
          {children.map((child) => (
            <FolderNode
              key={child.id}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              selected={selected}
              onSelect={onSelect}
              onToggle={onToggle}
              onCreateChild={onCreateChild}
              onRename={onRename}
              onDelete={onDelete}
              onDrop={onDrop}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

function FolderFormDialog({
  folder,
  parentId,
  busy,
  onCancel,
  onSave,
}: {
  folder: FolderTreeNode | null;
  parentId: number | null;
  busy: boolean;
  onCancel: () => void;
  onSave: (name: string, parentId: number | null, folder: FolderTreeNode | null) => Promise<void>;
}) {
  const [name, setName] = useState(folder?.name ?? "");
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-overlay/40" onClick={onCancel} role="presentation" />
      <form
        className="card relative w-full max-w-sm p-4"
        role="dialog"
        aria-modal="true"
        aria-labelledby="folder-dialog-title"
        onSubmit={(event) => {
          event.preventDefault();
          if (name.trim()) void onSave(name.trim(), parentId, folder);
        }}
      >
        <h3 id="folder-dialog-title" className="text-[14px] font-semibold">
          {folder ? "重命名文件夹" : parentId ? "创建子文件夹" : "创建文件夹"}
        </h3>
        <label className="mt-3 block">
          <span className="label">名称</span>
          <input
            className="input"
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="例如：待注册"
            aria-label="文件夹名称"
          />
        </label>
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onCancel} disabled={busy}>
            取消
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || !name.trim()}>
            {busy && <Spinner />} {folder ? "保存" : "创建"}
          </button>
        </div>
      </form>
    </div>
  );
}
