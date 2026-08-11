import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "../lib/api";
import type { DomainListResult, Facets } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { DomainTable } from "../components/DomainTable";
import { DomainDrawer } from "../components/DomainDrawer";
import { ConfirmDialog, EmptyState, ErrorNotice, Spinner, cx, useToast } from "../components/ui";
import { STATUS_LABELS, STATUS_ORDER } from "../lib/format";

const SORT_OPTIONS = [
  { value: "", label: "默认（加入顺序）" },
  { value: "name", label: "域名" },
  { value: "status", label: "状态" },
  { value: "expiry", label: "到期时间" },
  { value: "last_checked", label: "最后查询" },
  { value: "next_check", label: "下次查询" },
];

const PAGE_SIZES = [10, 20, 50, 100];

export function DomainsPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [params, setParams] = useSearchParams();
  const toast = useToast();

  const search = params.get("search") ?? "";
  const status = params.get("status") ?? "";
  const tld = params.get("tld") ?? "";
  const registrar = params.get("registrar") ?? "";
  const provider = params.get("provider") ?? "";
  const sort = params.get("sort") ?? "";
  const order = params.get("order") ?? "asc";
  const favoriteOnly = params.get("favorite") === "true";
  const page = Number(params.get("page") ?? "1") || 1;
  const limit = Number(params.get("limit") ?? "20") || 20;

  const [searchInput, setSearchInput] = useState(search);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [openDomain, setOpenDomain] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null);
  const [showAdd, setShowAdd] = useState(false);

  useEffect(() => setSearchInput(search), [search]);

  const query = useMemo(() => {
    const q = new URLSearchParams();
    if (search) q.set("search", search);
    if (status) q.set("status", status);
    if (tld) q.set("tld", tld);
    if (registrar) q.set("registrar", registrar);
    if (provider) q.set("provider", provider);
    if (sort) {
      q.set("sort", sort);
      q.set("order", order);
    }
    if (favoriteOnly) q.set("favorite", "true");
    q.set("page", String(page));
    q.set("limit", String(limit));
    return q.toString();
  }, [search, status, tld, registrar, provider, sort, order, page, limit, favoriteOnly]);

  const { data, error, loading, reload } = useAsync<DomainListResult>(
    () => api.get<DomainListResult>(`/api/v2/domains?${query}`),
    [query],
    onUnauthorized,
  );

  const domains = useMemo(() => data?.domains ?? [], [data]);

  // 筛选项来自全量统计，翻页不会让下拉框内容跟着变
  const facets = useAsync<Facets>(() => api.get<Facets>("/api/v2/facets"), [], onUnauthorized);

  // 列表和筛选项要一起刷新，否则新增/删除后下拉框的数量会对不上
  const refresh = useCallback(() => {
    reload();
    facets.reload();
  }, [reload, facets]);

  const updateParams = useCallback(
    (next: Record<string, string>) => {
      const merged = new URLSearchParams(params);
      Object.entries(next).forEach(([key, value]) => {
        if (value) merged.set(key, value);
        else merged.delete(key);
      });
      if (!("page" in next)) merged.set("page", "1");
      setParams(merged, { replace: true });
    },
    [params, setParams],
  );

  const tldOptions = facets.data?.tlds ?? [];
  const registrarOptions = facets.data?.registrars ?? [];
  const statusCounts = useMemo(() => {
    const map = new Map<string, number>();
    (facets.data?.statuses ?? []).forEach((item) => map.set(item.value, item.count));
    return map;
  }, [facets.data]);

  async function runCheck(name: string) {
    setBusy(name);
    try {
      await api.post(`/api/v2/domains/${encodeURIComponent(name)}/check`);
      toast(`${name} 查询完成`, "success");
      refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : "查询失败", "error");
    } finally {
      setBusy(null);
    }
  }

  async function runBatchCheck() {
    const names = Array.from(selected);
    if (names.length === 0) return;
    try {
      await api.post("/api/v2/domains/batch-check", { domains: names });
      toast(`已把 ${names.length} 个域名加入高优先级队列`, "success");
      setSelected(new Set());
      window.setTimeout(refresh, 2500);
    } catch (err) {
      toast(err instanceof Error ? err.message : "批量检查失败", "error");
    }
  }

  async function confirmDelete() {
    if (!pendingDelete) return;
    try {
      if (pendingDelete.length === 1) {
        await api.delete(`/api/v2/domains/${encodeURIComponent(pendingDelete[0])}`);
      } else {
        await api.post("/api/v2/domains/batch-delete", { domains: pendingDelete });
      }
      toast(`已删除 ${pendingDelete.length} 个域名`, "success");
      setSelected(new Set());
      refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : "删除失败", "error");
    } finally {
      setPendingDelete(null);
    }
  }

  return (
    <div className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-[18px] font-semibold tracking-tight">域名</h1>
          <p className="text-[12px] text-ink-muted">
            共 {data?.total ?? 0} 个域名
            {data && data.total_filtered !== data.total ? ` · 筛选出 ${data.total_filtered} 个` : ""}
          </p>
        </div>
        <div className="flex gap-2">
          <button type="button" className="btn h-8" onClick={refresh}>
            刷新
          </button>
          <button type="button" className="btn btn-primary h-8" onClick={() => setShowAdd(true)}>
            添加域名
          </button>
        </div>
      </header>

      <div className="card grid gap-2 p-3 sm:grid-cols-2 lg:grid-cols-6">
        <form
          className="sm:col-span-2 lg:col-span-2"
          onSubmit={(event) => {
            event.preventDefault();
            updateParams({ search: searchInput.trim() });
          }}
        >
          <input
            className="input"
            placeholder="搜索域名…"
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
          />
        </form>

        <select
          className="input"
          value={status}
          onChange={(event) => updateParams({ status: event.target.value })}
        >
          <option value="">全部状态</option>
          {STATUS_ORDER.map((value) => {
            const count = statusCounts.get(value);
            if (!count && status !== value) return null;
            return (
              <option key={value} value={value}>
                {STATUS_LABELS[value]}
                {count ? `（${count}）` : ""}
              </option>
            );
          })}
        </select>

        <select className="input" value={tld} onChange={(event) => updateParams({ tld: event.target.value })}>
          <option value="">全部后缀（{tldOptions.length}）</option>
          {tldOptions.map((item) => (
            <option key={item.value} value={item.value}>
              .{item.value}（{item.count}）
            </option>
          ))}
        </select>

        <select
          className="input"
          value={registrar}
          onChange={(event) => updateParams({ registrar: event.target.value })}
        >
          <option value="">全部注册商（{registrarOptions.length}）</option>
          {registrarOptions.map((item) => (
            <option key={item.value} value={item.value}>
              {item.value}（{item.count}）
            </option>
          ))}
        </select>

        <div className="flex gap-2">
          <select
            className="input"
            value={sort}
            onChange={(event) => updateParams({ sort: event.target.value })}
          >
            {SORT_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="btn shrink-0"
            onClick={() => updateParams({ order: order === "asc" ? "desc" : "asc" })}
            aria-label="切换排序方向"
          >
            {order === "asc" ? "↑" : "↓"}
          </button>
          <button
            type="button"
            className={cx("btn shrink-0", favoriteOnly && "border-accent text-accent")}
            onClick={() => updateParams({ favorite: favoriteOnly ? "" : "true" })}
            title="只看收藏"
            aria-pressed={favoriteOnly}
          >
            ★
          </button>
        </div>
      </div>

      {selected.size > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-accent/30 bg-accent-soft px-3 py-2 text-[13px]">
          <span>已选择 {selected.size} 个域名</span>
          <button type="button" className="btn h-7 text-[12px]" onClick={runBatchCheck}>
            批量检查
          </button>
          <button
            type="button"
            className="btn btn-danger h-7 text-[12px]"
            onClick={() => setPendingDelete(Array.from(selected))}
          >
            批量删除
          </button>
          <button
            type="button"
            className="btn btn-ghost h-7 text-[12px]"
            onClick={() => setSelected(new Set())}
          >
            取消选择
          </button>
        </div>
      )}

      {error && <ErrorNotice message={error} onRetry={refresh} />}

      {loading && domains.length === 0 ? (
        <div className="flex items-center gap-2 py-12 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      ) : domains.length === 0 ? (
        <div className="card">
          <EmptyState
            title={data?.data_status === "empty" ? "还没有添加任何域名" : "没有匹配的域名"}
            hint={data?.data_status === "empty" ? '点击右上角"添加域名"开始监控' : "试试调整筛选条件"}
          />
        </div>
      ) : (
        <DomainTable
          domains={domains}
          selected={selected}
          busy={busy}
          onToggle={(name) =>
            setSelected((current) => {
              const next = new Set(current);
              if (next.has(name)) next.delete(name);
              else next.add(name);
              return next;
            })
          }
          onToggleAll={(checked) =>
            setSelected(checked ? new Set(domains.map((item) => item.name)) : new Set())
          }
          onOpen={setOpenDomain}
          onCheck={runCheck}
          onDelete={(name) => setPendingDelete([name])}
        />
      )}

      {data && data.total_pages > 1 && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-[12px] text-ink-muted">
          <div className="flex items-center gap-2">
            <span>每页</span>
            <select
              className="input h-7 w-[72px] py-0"
              value={limit}
              onChange={(event) => updateParams({ limit: event.target.value })}
            >
              {PAGE_SIZES.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              className="btn h-7 px-2"
              disabled={!data.has_prev}
              onClick={() => updateParams({ page: String(page - 1) })}
            >
              上一页
            </button>
            <span className="tabular">
              {data.page} / {data.total_pages}
            </span>
            <button
              type="button"
              className="btn h-7 px-2"
              disabled={!data.has_next}
              onClick={() => updateParams({ page: String(page + 1) })}
            >
              下一页
            </button>
          </div>
        </div>
      )}

      <DomainDrawer
        domain={openDomain}
        onClose={() => setOpenDomain(null)}
        onChanged={refresh}
        onUnauthorized={onUnauthorized}
      />

      <ConfirmDialog
        open={Boolean(pendingDelete)}
        title={pendingDelete && pendingDelete.length > 1 ? "批量删除域名" : "删除域名"}
        description={
          pendingDelete
            ? `将删除 ${pendingDelete.length} 个域名及其查询结果、观测历史与通知记录，此操作不可撤销。`
            : ""
        }
        confirmText="删除"
        danger
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />

      {showAdd && (
        <AddDomainsDialog
          onClose={() => setShowAdd(false)}
          onDone={() => {
            setShowAdd(false);
            refresh();
          }}
        />
      )}
    </div>
  );
}

function AddDomainsDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [text, setText] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const toast = useToast();

  const names = text
    .split(/[\s,;]+/)
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean);

  async function submit() {
    if (names.length === 0) return;
    setSubmitting(true);
    try {
      if (names.length === 1) {
        const result = await api.post<{ status: string; message?: string }>("/api/v2/domains", {
          domain: names[0],
        });
        if (result.status !== "success") {
          toast(result.message || "添加失败", "error");
          setSubmitting(false);
          return;
        }
        toast("已添加并加入查询队列", "success");
      } else {
        const result = await api.post<{
          added_count: number;
          invalid_count: number;
          unsupported_count: number;
          duplicate_count: number;
        }>("/api/v2/domains/batch-add", { domains: names });
        toast(
          `新增 ${result.added_count} 个；重复 ${result.duplicate_count}，非法 ${result.invalid_count}，不支持 ${result.unsupported_count}`,
          "success",
        );
      }
      onDone();
    } catch (err) {
      toast(err instanceof Error ? err.message : "添加失败", "error");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} role="presentation" />
      <div className="card relative w-full max-w-lg p-4" role="dialog" aria-modal="true">
        <h3 className="text-[14px] font-semibold">添加域名</h3>
        <p className="mt-1 text-[12px] text-ink-muted">
          支持一次粘贴多个，用空格、换行、逗号或分号分隔，单次最多 1000 个。
        </p>
        <textarea
          className={cx("input mt-3 h-40 resize-y font-mono text-[12px]")}
          placeholder={"example.com\nexample.im"}
          value={text}
          onChange={(event) => setText(event.target.value)}
        />
        <div className="mt-1 text-[12px] text-ink-faint">已识别 {names.length} 个域名</div>
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={submit}
            disabled={submitting || names.length === 0}
          >
            {submitting && <Spinner />}添加
          </button>
        </div>
      </div>
    </div>
  );
}
