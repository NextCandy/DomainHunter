import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { api, downloadBlob } from "../lib/api";
import type { DomainListResult, Facets, FilterNode } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import { DomainTable } from "../components/DomainTable";
import type { DomainColumn, DomainSort } from "../components/DomainTable";
import { DomainDrawer } from "../components/DomainDrawer";
import { FolderTree } from "../components/FolderTree";
import type { FolderSelection } from "../components/FolderTree";
import { ImportExportDialog } from "../components/ImportExportDialog";
import { AdvancedFilterDrawer } from "../components/AdvancedFilterDrawer";
import { SavedViewMenu } from "../components/SavedViewMenu";
import { BulkActionPreviewDialog } from "../components/BulkActionPreviewDialog";
import { ConfirmDialog, EmptyState, ErrorNotice, Skeleton, Spinner, cx, useDensity, useToast } from "../components/ui";
import { STATUS_LABELS, STATUS_ORDER } from "../lib/format";

const SORT_OPTIONS = [
  { value: "", label: "默认（加入顺序）" },
  { value: "name", label: "域名" },
  { value: "status", label: "状态" },
  { value: "expiry", label: "到期时间" },
  { value: "last_checked", label: "最后查询" },
  { value: "next_check", label: "下次查询" },
  { value: "ai_score", label: "AI 评分" },
];

const PAGE_SIZES = [20, 50, 100, 1000];
const VIEW_STORAGE_KEY = "dh-domains-view-v1";
const ALL_COLUMNS: Array<{ value: DomainColumn; label: string }> = [
  { value: "status", label: "状态" },
  { value: "registrar", label: "注册商" },
  { value: "expiry", label: "到期时间" },
  { value: "provider", label: "查询来源" },
  { value: "ai_score", label: "AI 评分" },
  { value: "last_checked", label: "最后查询" },
  { value: "next_check", label: "下次查询" },
];

export function DomainsPage({ onUnauthorized }: { onUnauthorized: () => void }) {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const toast = useToast();

  const search = params.get("search") ?? "";
  const status = params.get("status") ?? "";
  const tld = params.get("tld") ?? "";
  const registrar = params.get("registrar") ?? "";
  const provider = params.get("provider") ?? "";
  const statuses = params.get("statuses") ?? "";
  const sort = params.get("sort") ?? "";
  const order = params.get("order") ?? "asc";
  const favoriteOnly = params.get("favorite") === "true";
  const advancedRaw = params.get("filter") ?? "";
  const columnsParam = params.get("columns") ?? "";
  const page = Number(params.get("page") ?? "1") || 1;
  const limit = Number(params.get("limit") ?? "20") || 20;

  const [searchInput, setSearchInput] = useState(search);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [openDomain, setOpenDomain] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [showImportExport, setShowImportExport] = useState(false);
  const [folderSelection, setFolderSelection] = useState<FolderSelection>("all");
  const [retrying, setRetrying] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [showBulkAction, setShowBulkAction] = useState(false);
  const [moreOpen, setMoreOpen] = useState(false);
  const [columnMenuOpen, setColumnMenuOpen] = useState(false);
  const { density, setDensity } = useDensity();
  const [visibleColumns, setVisibleColumns] = useState<Set<DomainColumn>>(
    () => {
      const fromURL = columnsParam.split(",").filter((item): item is DomainColumn => ALL_COLUMNS.some((column) => column.value === item));
      if (fromURL.length > 0) return new Set(fromURL);
      try {
        const stored = JSON.parse(localStorage.getItem(VIEW_STORAGE_KEY) ?? "{}") as { columns?: DomainColumn[] };
        if (Array.isArray(stored.columns)) return new Set(stored.columns);
      } catch { /* 使用默认列 */ }
      return new Set(ALL_COLUMNS.map((item) => item.value));
    },
  );
  const restoredView = useRef(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const [keyboardIndex, setKeyboardIndex] = useState(-1);

  const advancedFilter = useMemo<FilterNode>(() => {
    if (!advancedRaw) return { version: 1, logic: "and", conditions: [] };
    try { return JSON.parse(advancedRaw) as FilterNode; } catch { return { version: 1, logic: "and", conditions: [] }; }
  }, [advancedRaw]);

  useEffect(() => setSearchInput(search), [search]);

  useEffect(() => {
    const requested = params.get("density");
    if (requested === "comfortable" || requested === "compact" || requested === "dense") {
      if (requested !== density) setDensity(requested);
    }
  }, [density, params, setDensity]);

  useEffect(() => {
    const encoded = Array.from(visibleColumns).join(",");
    if (encoded === columnsParam) return;
    const next = new URLSearchParams(params);
    if (encoded) next.set("columns", encoded); else next.delete("columns");
    setParams(next, { replace: true });
  }, [columnsParam, params, setParams, visibleColumns]);

  useEffect(() => {
    if (!columnsParam) return;
    const requested = new Set(columnsParam.split(",").filter((item): item is DomainColumn => ALL_COLUMNS.some((column) => column.value === item)));
    if (requested.size === 0 || requested.size !== visibleColumns.size || Array.from(requested).some((item) => !visibleColumns.has(item))) {
      setVisibleColumns(requested.size > 0 ? requested : new Set(ALL_COLUMNS.map((item) => item.value)));
    }
  }, [columnsParam, visibleColumns]);

  useEffect(() => {
    if (restoredView.current) return;
    restoredView.current = true;
    try {
      const stored = JSON.parse(localStorage.getItem(VIEW_STORAGE_KEY) ?? "{}") as { query?: string };
      if (!params.toString() && stored.query) setParams(new URLSearchParams(stored.query), { replace: true });
    } catch { /* 保持默认视图 */ }
  }, [params, setParams]); // 首次进入时才恢复；用户主动清空筛选后不能被旧状态覆盖。

  useEffect(() => {
    try {
      localStorage.setItem(VIEW_STORAGE_KEY, JSON.stringify({ query: params.toString(), columns: Array.from(visibleColumns) }));
    } catch { /* localStorage 不可用时 URL 仍是事实来源 */ }
  }, [params, visibleColumns]);

  const query = useMemo(() => {
    const q = new URLSearchParams();
    if (search) q.set("search", search);
    if (status) q.set("status", status);
    if (tld) q.set("tld", tld);
    if (registrar) q.set("registrar", registrar);
    if (provider) q.set("provider", provider);
    if (statuses) q.set("statuses", statuses);
    if (sort) {
      q.set("sort", sort);
      q.set("order", order);
    }
    if (favoriteOnly) q.set("favorite", "true");
    if (advancedRaw) q.set("filter", advancedRaw);
    q.set("page", String(page));
    q.set("limit", String(folderSelection === "all" ? limit : 2000));
    return q.toString();
  }, [search, status, statuses, tld, registrar, provider, sort, order, page, limit, favoriteOnly, advancedRaw, folderSelection]);

  const { data, error, loading, reload } = useAsync<DomainListResult>(
    () => api.get<DomainListResult>(`/api/v2/domains?${query}`),
    [query],
    onUnauthorized,
  );

  const domains = useMemo(() => {
    let items = data?.domains ?? [];
    if (sort === "ai_score") {
      items = [...items].sort((a, b) => {
        const left = a.ai_quality_score ?? -1;
        const right = b.ai_quality_score ?? -1;
        return order === "desc" ? right - left : left - right;
      });
    }
    if (folderSelection === "all") return items;
    return items.filter((item) =>
      folderSelection === "root" ? item.folder_id == null : item.folder_id === folderSelection,
    );
  }, [data, folderSelection, order, sort]);

  // 筛选项来自全量统计，翻页不会让下拉框内容跟着变
  const facets = useAsync<Facets>(() => api.get<Facets>("/api/v2/facets"), [], onUnauthorized);
  const reloadFacets = facets.reload;

  // 列表和筛选项要一起刷新，否则新增/删除后下拉框的数量会对不上
  const refresh = useCallback(() => {
    reload();
    reloadFacets();
  }, [reload, reloadFacets]);

  useEffect(() => {
    if (params.get("focus") === "search") {
      searchRef.current?.focus();
      const next = new URLSearchParams(params);
      next.delete("focus");
      setParams(next, { replace: true });
    }
  }, [params, setParams]);

  useEffect(() => {
    if (params.get("new") !== "1") return;
    setShowAdd(true);
    const next = new URLSearchParams(params);
    next.delete("new");
    setParams(next, { replace: true });
  }, [params, setParams]);

  useEffect(() => {
    const clearSelection = () => setSelected(new Set());
    window.addEventListener("domainhunter:clear-selection", clearSelection);
    return () => window.removeEventListener("domainhunter:clear-selection", clearSelection);
  }, []);

  useEffect(() => {
    const onRefresh = () => refresh();
    const onKey = (event: KeyboardEvent) => {
      const target = event.target;
      if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return;
      if (event.key === "j" || event.key === "k") {
        event.preventDefault();
        setKeyboardIndex((current) => event.key === "j" ? Math.min(domains.length - 1, current + 1) : Math.max(0, current - 1));
      } else if (event.key === "Enter" && keyboardIndex >= 0 && domains[keyboardIndex]) {
        setOpenDomain(domains[keyboardIndex].name);
      }
    };
    window.addEventListener("domainhunter:refresh", onRefresh);
    window.addEventListener("keydown", onKey);
    return () => { window.removeEventListener("domainhunter:refresh", onRefresh); window.removeEventListener("keydown", onKey); };
  }, [domains, keyboardIndex, refresh]);

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
  const failedCount = (statusCounts.get("error") ?? 0) + (statusCounts.get("unknown") ?? 0);
  const failedOnly = statuses === "error,unknown";
  const filterChips = [
    search ? { key: "search", label: `搜索：${search}` } : null,
    status ? { key: "status", label: `状态：${STATUS_LABELS[status as keyof typeof STATUS_LABELS] ?? status}` } : null,
    tld ? { key: "tld", label: `后缀：.${tld.replace(/^\./, "")}` } : null,
    registrar ? { key: "registrar", label: `注册商：${registrar}` } : null,
    provider ? { key: "provider", label: `查询源：${provider}` } : null,
    statuses ? { key: "statuses", label: failedOnly ? "失败 / 未知" : `状态组：${statuses}` } : null,
    favoriteOnly ? { key: "favorite", label: "仅收藏" } : null,
    advancedRaw ? { key: "filter", label: "高级条件" } : null,
  ].filter((item): item is { key: string; label: string } => Boolean(item));

  function changeSort(field: DomainSort) {
    if (!field) return;
    updateParams({ sort: field, order: sort === field && order === "asc" ? "desc" : "asc" });
  }

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

  async function runBatchRetryFailed() {
    setRetrying(true);
    try {
      const result = await api.domains.batchRetryFailed();
      toast(result.message || `已将 ${result.queued} 个失败域名加入重试队列`, "success");
      window.setTimeout(refresh, 1200);
    } catch (err) {
      toast(err instanceof Error ? err.message : "批量重试失败", "error");
    } finally {
      setRetrying(false);
    }
  }

  async function moveDomainsToFolder(folderId: number | null, names: string[]) {
    try {
      const result = await api.domains.batchMoveFolder(names, folderId);
      toast(`已移动 ${result.moved} 个域名`, "success");
      setSelected(new Set());
      refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : "移动域名失败", "error");
    }
  }

  function exportSelected() {
    const names = Array.from(selected);
    if (names.length === 0) return;
    const byName = new Map(domains.map((item) => [item.name, item]));
    const escape = (value: unknown) => `"${String(value ?? "").replace(/"/g, '""')}"`;
    const rows = [
      ["domain", "status", "registrar", "expiry_date", "query_method", "last_checked"].join(","),
      ...names.map((name) => {
        const item = byName.get(name);
        return [name, item?.status, item?.registrar, item?.expiry_date, item?.query_method, item?.last_checked].map(escape).join(",");
      }),
    ];
    downloadBlob(new Blob(["\uFEFF", rows.join("\n")], { type: "text/csv;charset=utf-8" }), "domainhunter-selected.csv");
    toast(`已导出 ${names.length} 个选中域名`, "success");
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
      <header className="workspace-header flex flex-wrap items-center justify-between gap-2">
        <div>
          <span className="workspace-kicker">DOMAIN OPERATIONS</span>
          <h1 className="mt-1 text-[28px] font-semibold tracking-tight">域名资产</h1>
          <p className="text-[12px] text-ink-muted">
            {folderSelection === "all" ? `共 ${data?.total ?? 0} 个域名` : `当前文件夹 ${domains.length} 个域名`}
            {folderSelection === "all" && data && data.total_filtered !== data.total
              ? ` · 筛选出 ${data.total_filtered} 个`
              : ""}
          </p>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className="btn min-h-11 min-w-11 px-0 sm:min-h-9 sm:min-w-9" onClick={refresh} aria-label="刷新域名列表" title="刷新域名列表">
            <RefreshIcon />
          </button>
          <button type="button" className={cx("btn min-h-11 sm:min-h-9", filterChips.length > 0 && "border-accent text-accent")} onClick={() => setShowAdvanced(true)}>
            高级筛选{filterChips.length > 0 ? `（${filterChips.length}）` : ""}
          </button>
          <div className="relative">
            <button type="button" className="btn min-h-11 sm:min-h-9" onClick={() => setMoreOpen((value) => !value)} aria-expanded={moreOpen}>更多 <span aria-hidden="true">⌄</span></button>
            {moreOpen && (
              <div className="absolute right-0 top-12 z-30 w-52 rounded-card border border-line bg-surface-raised p-2 shadow-lg sm:top-10">
                <SavedViewMenu filter={advancedFilter} onApply={(filter) => { updateParams({ filter: JSON.stringify(filter) }); setMoreOpen(false); }} onUnauthorized={onUnauthorized} embedded />
                <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[12px] hover:bg-surface-muted" onClick={() => { setShowImportExport(true); setMoreOpen(false); }}>导入 / 导出</button>
                <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[12px] hover:bg-surface-muted" onClick={() => void runBatchRetryFailed()} disabled={retrying}>{retrying && <Spinner />} 重试失败{failedCount > 0 ? `（${failedCount}）` : ""}</button>
              </div>
            )}
          </div>
          <button
            type="button"
            className="btn btn-primary min-h-11 sm:min-h-9"
            onClick={() => setShowAdd(true)}
            aria-label="添加域名"
            title="添加域名"
          >
            添加域名
          </button>
        </div>
      </header>

      <div className="grid gap-4 lg:grid-cols-[minmax(190px,240px)_minmax(0,1fr)]">
        <FolderTree
          selected={folderSelection}
          onSelect={setFolderSelection}
          onDomainDrop={moveDomainsToFolder}
          onUnauthorized={onUnauthorized}
        />
        <div className="min-w-0 space-y-4">
          <div className="card grid gap-2 p-3 sm:grid-cols-2 lg:grid-cols-6">
        <form
          className="sm:col-span-2 lg:col-span-2"
          onSubmit={(event) => {
            event.preventDefault();
            updateParams({ search: searchInput.trim() });
          }}
        >
          <input
            ref={searchRef}
            className="input"
            placeholder="搜索域名…"
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
          />
        </form>

        <select
          className="input"
          aria-label="按状态筛选"
          value={status}
          onChange={(event) => updateParams({ status: event.target.value, statuses: "" })}
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

        <select className="input" aria-label="按后缀筛选" value={tld} onChange={(event) => updateParams({ tld: event.target.value })}>
          <option value="">全部后缀（{tldOptions.length}）</option>
          {tldOptions.map((item) => (
            <option key={item.value} value={item.value}>
              .{item.value}（{item.count}）
            </option>
          ))}
        </select>

        <select
          className="input"
          aria-label="按注册商筛选"
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
            aria-label="排序字段"
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
          <button
            type="button"
            className={cx("btn shrink-0 text-[12px]", failedOnly && "border-accent text-accent")}
            onClick={() => updateParams({ statuses: failedOnly ? "" : "error,unknown", status: "" })}
            aria-label={failedOnly ? "取消失败域名筛选" : "筛选失败和未知域名"}
            title="筛选 error 和 unknown 状态"
            aria-pressed={failedOnly}
          >
            失败/未知
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-2 sm:col-span-2 lg:col-span-6">
          {filterChips.map((chip) => (
            <button key={chip.key} type="button" className="inline-flex min-h-8 items-center gap-2 rounded-tag border border-accent/20 bg-accent-soft/45 px-3 text-[11px] text-ink" onClick={() => updateParams({ [chip.key]: "" })} title={`移除${chip.label}`}>
              {chip.label}<span aria-hidden="true">×</span>
            </button>
          ))}
          {filterChips.length > 0 && <button type="button" className="min-h-8 px-2 text-[11px] text-accent hover:underline" onClick={() => setParams(new URLSearchParams(), { replace: true })}>清空全部</button>}
          <span className="self-center text-[11px] text-ink-faint">高级条件支持状态、TLD、注册商、标签和 AI 质量分。</span>
        </div>
      </div>

      <div className="relative flex justify-end">
        <button type="button" className="btn min-h-9 text-[12px]" onClick={() => setColumnMenuOpen((value) => !value)} aria-expanded={columnMenuOpen}>列设置</button>
        {columnMenuOpen && (
          <div className="absolute right-0 top-10 z-20 grid w-48 gap-1 rounded-card border border-line bg-surface-raised p-2 shadow-lg">
            {ALL_COLUMNS.map((column) => (
              <label key={column.value} className="flex min-h-10 items-center gap-2 rounded-md px-2 text-[12px] hover:bg-surface-muted">
                <input type="checkbox" checked={visibleColumns.has(column.value)} onChange={() => setVisibleColumns((current) => { if (current.has(column.value) && current.size === 1) return current; const next = new Set(current); if (next.has(column.value)) next.delete(column.value); else next.add(column.value); return next; })} />
                {column.label}
              </label>
            ))}
          </div>
        )}
      </div>

      {failedCount > 0 && (
        <p className="rounded-card border border-line bg-surface-muted px-4 py-3 text-[12px] text-ink-muted">
          当前有 {failedCount} 个失败或未知域名；“重试失败”会由后端在 30 秒窗口内均摊排队，可先用“失败/未知”筛选查看。
        </p>
      )}

      {selected.size > 0 && (
        <div className="fixed inset-x-3 bottom-20 z-40 mx-auto flex max-w-4xl flex-wrap items-center justify-center gap-2 rounded-card border border-accent/30 bg-surface-raised p-2 text-[13px] shadow-lg lg:bottom-6">
          <span className="rounded-button bg-accent px-3 py-2 text-on-accent">已选择 {selected.size} 项</span>
          <button type="button" className="btn min-h-10 text-[12px]" onClick={runBatchCheck}>立即检查</button>
          <button type="button" className="btn min-h-10 text-[12px]" onClick={() => setShowBulkAction(true)}>打标签 / 移入文件夹</button>
          <button type="button" className="btn min-h-10 text-[12px]" onClick={exportSelected}>导出 CSV</button>
          <button type="button" className="btn min-h-10 text-[12px] text-danger" onClick={() => setPendingDelete(Array.from(selected))}>删除</button>
          <button type="button" className="btn btn-ghost min-h-10 min-w-10 px-0" onClick={() => setSelected(new Set())} aria-label="取消选择">×</button>
        </div>
      )}

      {error && <ErrorNotice message={error} onRetry={refresh} />}

      {loading && domains.length === 0 ? (
        <div className="space-y-3" aria-label="域名列表加载中">
          <Skeleton className="h-12 w-full rounded-card" />
          <div className="card overflow-hidden">{Array.from({ length: 7 }, (_, index) => <div key={index} className="flex items-center gap-3 border-b border-line p-4 last:border-0"><Skeleton className="h-4 w-4 rounded" /><Skeleton className="h-4 w-44" /><Skeleton className="ml-auto h-4 w-20" /><Skeleton className="h-4 w-24" /></div>)}</div>
        </div>
      ) : domains.length === 0 ? (
        <div className="card">
          <EmptyState
            title={data?.data_status === "empty" ? "还没有添加任何域名" : "没有匹配的域名"}
            hint={data?.data_status === "empty" ? '点击右上角"添加域名"开始监控' : "试试调整筛选条件"}
            action={
              data?.data_status === "empty" ? (
                <button type="button" className="btn btn-primary min-h-10" onClick={() => setShowAdd(true)}>添加第一个域名</button>
              ) : (
                <button type="button" className="btn min-h-10" onClick={() => setParams(new URLSearchParams(), { replace: true })}>清空筛选</button>
              )
            }
          />
        </div>
      ) : (
        <DomainTable
          domains={domains}
          selected={selected}
          busy={busy}
          visibleColumns={visibleColumns}
          sort={sort as DomainSort}
          order={order === "desc" ? "desc" : "asc"}
          keyboardIndex={keyboardIndex}
          onSort={changeSort}
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
          onHistory={(name) => navigate(`/history?domain=${encodeURIComponent(name)}`)}
        />
      )}

      {folderSelection === "all" && data && data.total_pages > 1 && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-[12px] text-ink-muted">
          <div className="flex items-center gap-2">
            <span>每页</span>
            <select
              className="input h-7 w-[72px] py-0"
              aria-label="每页数量"
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
        </div>
      </div>

      <DomainDrawer
        domain={openDomain}
        domains={domains.map((item) => item.name)}
        onNavigate={setOpenDomain}
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
      {showImportExport && (
        <ImportExportDialog
          onClose={() => setShowImportExport(false)}
          onDone={refresh}
          onUnauthorized={onUnauthorized}
        />
      )}
      <AdvancedFilterDrawer
        open={showAdvanced}
        initial={advancedFilter}
        onApply={(filter) => updateParams({ filter: JSON.stringify(filter) })}
        onClose={() => setShowAdvanced(false)}
      />
      <BulkActionPreviewDialog
        open={showBulkAction}
        domains={Array.from(selected)}
        onClose={() => setShowBulkAction(false)}
        onDone={() => { setSelected(new Set()); refresh(); }}
      />
    </div>
  );
}

function RefreshIcon() {
  return <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><path d="M13.2 5.5A5.5 5.5 0 1 0 13 10.9" /><path d="M10.5 5.5h2.75V2.75" /></svg>;
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
      <div className="absolute inset-0 bg-overlay/40" onClick={onClose} role="presentation" />
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
