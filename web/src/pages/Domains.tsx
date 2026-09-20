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
import { SearchableMultiSelect } from "../components/SearchableMultiSelect";
import type { SelectOption } from "../components/SearchableMultiSelect";
import { ConfirmDialog, EmptyState, ErrorNotice, Skeleton, Spinner, cx, useDensity, useIsMobile, useToast } from "../components/ui";
import { STATUS_LABELS, STATUS_ORDER } from "../lib/format";
import { registrarShortName } from "../lib/registrar";

/**
 * 筛选参数读取：多值参数（statuses/tlds/registrars）优先，
 * 回落到旧的单值参数（status/tld/registrar），这样别人分享出去的旧链接仍然有效。
 */
function readMulti(multi: string | null, single: string | null): string[] {
  const fromMulti = (multi ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  if (fromMulti.length > 0) return fromMulti;
  const one = (single ?? "").trim();
  return one ? [one] : [];
}

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
const FOLDER_COLLAPSE_KEY = "dh-domains-folder-collapsed";
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
  const provider = params.get("provider") ?? "";
  const statusList = useMemo(
    () => readMulti(params.get("statuses"), params.get("status")),
    [params],
  );
  const tldList = useMemo(() => readMulti(params.get("tlds"), params.get("tld")), [params]);
  const registrarList = useMemo(
    () => readMulti(params.get("registrars"), params.get("registrar")),
    [params],
  );
  const sort = params.get("sort") ?? "";
  const order = params.get("order") ?? "asc";
  const favoriteOnly = params.get("favorite") === "true";
  // 「需要复核」是证据维度而不是状态维度，单独一个参数。
  const reviewOnly = params.get("review") === "1" || params.get("review") === "true";
  const advancedRaw = params.get("filter") ?? "";
  const columnsParam = params.get("columns") ?? "";
  const page = Number(params.get("page") ?? "1") || 1;
  // 每页 50：561 个域名分成 12 页比 29 页好翻，一屏也装得下。
  const limit = Number(params.get("limit") ?? "50") || 50;

  const [searchInput, setSearchInput] = useState(search);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [openDomain, setOpenDomain] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<string[] | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [showImportExport, setShowImportExport] = useState(false);
  const [folderSelection, setFolderSelection] = useState<FolderSelection>("all");
  const [retrying, setRetrying] = useState(false);
  const [valuating, setValuating] = useState(false);
  const [filterSheetOpen, setFilterSheetOpen] = useState(false);
  const isMobile = useIsMobile();
  // 文件夹树折叠状态记在本地：不用文件夹的人不该每次都被它占掉 240px。
  const [folderCollapsed, setFolderCollapsed] = useState(() => {
    try {
      return localStorage.getItem(FOLDER_COLLAPSE_KEY) === "1";
    } catch {
      return false;
    }
  });
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

  // 筛选项来自全量统计，翻页不会让下拉框内容跟着变
  const facets = useAsync<Facets>(() => api.get<Facets>("/api/v2/facets"), [], onUnauthorized);
  const reloadFacets = facets.reload;

  // 注册商全名长到能撑破列宽，列表里只放短名，全名进 title 和搜索关键词。
  //
  // 同一家注册商常常有多个法律实体——阿里云在库里就有三个（Alibaba Cloud
  // (Beijing)、阿里云计算有限公司（万网）、HiChina），压成短名后在列表里看起来
  // 是三个一模一样的"阿里云"，还没法区分选哪个。按短名合并成一个选项，选中时
  // 把这一组全名一起发给后端：用户心里的"阿里云"本来就是这三个的总和。
  const registrarGroups = useMemo(() => {
    const map = new Map<string, { names: string[]; count: number }>();
    for (const item of facets.data?.registrars ?? []) {
      const short = registrarShortName(item.value) || item.value;
      const entry = map.get(short) ?? { names: [], count: 0 };
      entry.names.push(item.value);
      entry.count += item.count;
      map.set(short, entry);
    }
    return map;
  }, [facets.data]);

  const query = useMemo(() => {
    const q = new URLSearchParams();
    if (search) q.set("search", search);
    // 一律用多值参数发给后端；后端对 statuses/tlds/registrars 的空值会回落到单值字段。
    if (statusList.length > 0) q.set("statuses", statusList.join(","));
    if (tldList.length > 0) q.set("tlds", tldList.join(","));
    // 选中的是短名，后端按全名精确匹配，这里展开成该短名下的全部法律实体。
    // 找不到分组说明来自旧链接里的全名，原样透传即可。
    if (registrarList.length > 0) {
      const names = registrarList.flatMap((short) => registrarGroups.get(short)?.names ?? [short]);
      q.set("registrars", names.join(","));
    }
    if (provider) q.set("provider", provider);
    if (sort) {
      q.set("sort", sort);
      q.set("order", order);
    }
    if (favoriteOnly) q.set("favorite", "true");
    if (reviewOnly) q.set("review", "1");
    if (advancedRaw) q.set("filter", advancedRaw);
    q.set("page", String(page));
    q.set("limit", String(folderSelection === "all" ? limit : 2000));
    return q.toString();
  }, [search, statusList, tldList, registrarList, registrarGroups, provider, sort, order, page, limit, favoriteOnly, reviewOnly, advancedRaw, folderSelection]);

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

  const statusCounts = useMemo(() => {
    const map = new Map<string, number>();
    (facets.data?.statuses ?? []).forEach((item) => map.set(item.value, item.count));
    return map;
  }, [facets.data]);
  const failedCount = (statusCounts.get("error") ?? 0) + (statusCounts.get("unknown") ?? 0);
  // 当前这一页一个评分都没有——AI 评分列全是「—」，光摆着没意义。
  const noScores = domains.length > 0 && domains.every((item) => item.ai_quality_score == null);
  // 「失败/未知」是状态多选的一个快捷方式，而不是另一个维度——
  // 两者共用 statuses 参数，按钮的选中态就是"选中的正好是这两项"。
  const failedOnly =
    statusList.length === 2 && statusList.includes("error") && statusList.includes("unknown");

  const statusOptions = useMemo<SelectOption[]>(
    () =>
      STATUS_ORDER.filter((value) => statusCounts.get(value) || statusList.includes(value)).map(
        (value) => ({
          value,
          label: STATUS_LABELS[value],
          count: statusCounts.get(value),
        }),
      ),
    [statusCounts, statusList],
  );

  const tldOptions = useMemo<SelectOption[]>(
    () =>
      (facets.data?.tlds ?? []).map((item) => ({
        value: item.value,
        label: `.${item.value}`,
        count: item.count,
      })),
    [facets.data],
  );

  const registrarOptions = useMemo<SelectOption[]>(
    () =>
      Array.from(registrarGroups, ([short, entry]) => ({
        value: short,
        label: short,
        count: entry.count,
        title: entry.names.join(" · "),
        keywords: entry.names.join(" "),
      })).sort((left, right) => (right.count ?? 0) - (left.count ?? 0)),
    [registrarGroups],
  );

  const toggleFolderPanel = useCallback((collapsed: boolean) => {
    setFolderCollapsed(collapsed);
    try {
      localStorage.setItem(FOLDER_COLLAPSE_KEY, collapsed ? "1" : "0");
    } catch {
      /* 存不下就只在本次会话生效 */
    }
  }, []);

  const setStatusList = useCallback(
    (next: string[]) => updateParams({ statuses: next.join(","), status: "" }),
    [updateParams],
  );
  const setTldList = useCallback(
    (next: string[]) => updateParams({ tlds: next.join(","), tld: "" }),
    [updateParams],
  );
  const setRegistrarList = useCallback(
    (next: string[]) => updateParams({ registrars: next.join(","), registrar: "" }),
    [updateParams],
  );

  type FilterChip = { key: string; label: string; onRemove: () => void };
  const filterChips: FilterChip[] = [
    ...(search ? [{ key: "search", label: `搜索：${search}`, onRemove: () => updateParams({ search: "" }) }] : []),
    ...statusList.map((value) => ({
      key: `status:${value}`,
      label: `状态：${STATUS_LABELS[value as keyof typeof STATUS_LABELS] ?? value}`,
      onRemove: () => setStatusList(statusList.filter((item) => item !== value)),
    })),
    ...tldList.map((value) => ({
      key: `tld:${value}`,
      label: `后缀：.${value.replace(/^\./, "")}`,
      onRemove: () => setTldList(tldList.filter((item) => item !== value)),
    })),
    ...registrarList.map((value) => ({
      key: `registrar:${value}`,
      label: `注册商：${registrarShortName(value) || value}`,
      onRemove: () => setRegistrarList(registrarList.filter((item) => item !== value)),
    })),
    ...(provider ? [{ key: "provider", label: `查询源：${provider}`, onRemove: () => updateParams({ provider: "" }) }] : []),
    ...(favoriteOnly ? [{ key: "favorite", label: "仅收藏", onRemove: () => updateParams({ favorite: "" }) }] : []),
    ...(reviewOnly ? [{ key: "review", label: "需要复核", onRemove: () => updateParams({ review: "" }) }] : []),
    ...(advancedRaw ? [{ key: "filter", label: "高级条件", onRemove: () => updateParams({ filter: "" }) }] : []),
  ];

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

  async function runBatchValuation(target?: string[]) {
    const names = target ?? Array.from(selected);
    if (names.length === 0) return;
    setValuating(true);
    try {
      const result = await api.domains.batchValuation(names);
      // 已有估价结果的直接命中缓存，不会重复消耗模型额度，分开报数好让人知道真排了多少。
      toast(
        `已入队 ${result.queued} 个${result.cached > 0 ? `，命中已有估价 ${result.cached} 个` : ""}${result.failed > 0 ? `，失败 ${result.failed} 个` : ""}`,
        result.failed > 0 ? "info" : "success",
      );
      if (!target) setSelected(new Set());
      refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : "加入估价队列失败", "error");
    } finally {
      setValuating(false);
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

  // 手机上七个筛选控件纵向堆起来会占掉整整一屏，桌面上它们排一行正好。
  // 同一组控件两种排布，用同一份 JSX，靠这几个类名开关切换。
  const selectClass = isMobile ? "w-full" : "w-[9rem] shrink-0";
  const wideSelectClass = isMobile ? "w-full" : "w-[11rem] shrink-0";
  const toggleClass = isMobile ? "min-h-11 flex-1" : "shrink-0";

  const filterControls = (
    <>
      <form
        className={isMobile ? "w-full" : "min-w-[180px] flex-1"}
        onSubmit={(event) => {
          event.preventDefault();
          updateParams({ search: searchInput.trim() });
          setFilterSheetOpen(false);
        }}
      >
        <input
          ref={searchRef}
          className={cx("input", isMobile && "min-h-11")}
          placeholder="搜索域名、注册商、标签…"
          value={searchInput}
          onChange={(event) => setSearchInput(event.target.value)}
        />
      </form>

      <SearchableMultiSelect
        className={selectClass}
        label="全部状态"
        searchPlaceholder="筛选状态…"
        options={statusOptions}
        selected={statusList}
        onChange={setStatusList}
      />

      <SearchableMultiSelect
        className={selectClass}
        label={`全部后缀（${tldOptions.length}）`}
        searchPlaceholder="输入后缀，如 com"
        options={tldOptions}
        selected={tldList}
        onChange={setTldList}
      />

      <SearchableMultiSelect
        className={wideSelectClass}
        label={`全部注册商（${registrarOptions.length}）`}
        searchPlaceholder="输入注册商名…"
        options={registrarOptions}
        selected={registrarList}
        onChange={setRegistrarList}
      />

      <div className={isMobile ? "flex w-full gap-2" : "contents"}>
        <button
          type="button"
          className={cx("btn text-[12px]", toggleClass, failedOnly && "border-accent text-accent")}
          onClick={() => setStatusList(failedOnly ? [] : ["error", "unknown"])}
          aria-label={failedOnly ? "取消失败域名筛选" : "筛选失败和未知域名"}
          title="筛选 error 和 unknown 状态"
          aria-pressed={failedOnly}
        >
          失败/未知
        </button>
        <button
          type="button"
          className={cx("btn gap-1", toggleClass, favoriteOnly && "border-accent text-accent")}
          onClick={() => updateParams({ favorite: favoriteOnly ? "" : "true" })}
          title="只看收藏"
          aria-label="只看收藏"
          aria-pressed={favoriteOnly}
        >
          <span aria-hidden="true">★</span>
          {/* 图标单独出现时没人知道它是筛选器，选中后补一个文字态说明当前在筛什么。 */}
          {favoriteOnly && <span className="text-[12px]">只看收藏</span>}
        </button>
        <button
          type="button"
          className={cx("btn text-[12px]", toggleClass, advancedRaw && "border-accent text-accent")}
          onClick={() => { setShowAdvanced(true); setFilterSheetOpen(false); }}
          title="高级条件支持状态、TLD、注册商、标签和 AI 质量分"
        >
          高级筛选
        </button>
      </div>
    </>
  );

  const chipsRow = filterChips.length > 0 && (
    <div className={cx("flex w-full flex-wrap items-center gap-2", !isMobile && "border-t border-line pt-2")}>
      {filterChips.map((chip) => (
        <button key={chip.key} type="button" className="chip chip-active" onClick={chip.onRemove} title={`移除${chip.label}`}>
          {chip.label}<span aria-hidden="true">×</span>
        </button>
      ))}
      <button type="button" className="min-h-8 px-2 text-[11px] text-accent hover:underline" onClick={() => setParams(new URLSearchParams(), { replace: true })}>清空全部</button>
    </div>
  );

  return (
    <div className="space-y-4">
      {/* 第一行只回答"我在哪、我要新增什么"：标题 + 总数 + 刷新，右侧一个菜单
          加一个主按钮。所有筛选类控件下沉到第二行，顶栏不再是四个平级按钮打架。 */}
      <header className="workspace-header flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <span className="workspace-kicker">DOMAIN OPERATIONS</span>
          <div className="mt-1 flex items-center gap-2">
            <h1>域名资产</h1>
            <button
              type="button"
              className="btn min-h-9 min-w-9 shrink-0 px-0"
              onClick={refresh}
              aria-label="刷新域名列表"
              title="刷新域名列表"
            >
              <RefreshIcon />
            </button>
          </div>
          <p className="text-[12px] text-ink-muted">
            {folderSelection === "all" ? `共 ${data?.total ?? 0} 个域名` : `当前文件夹 ${domains.length} 个域名`}
            {folderSelection === "all" && data && data.total_filtered !== data.total
              ? ` · 筛选出 ${data.total_filtered} 个`
              : ""}
          </p>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <div className="relative">
            <button type="button" className="btn min-h-11 sm:min-h-9" onClick={() => setMoreOpen((value) => !value)} aria-expanded={moreOpen}>导入 / 导出 <span aria-hidden="true">⌄</span></button>
            {moreOpen && (
              <div className="absolute right-0 top-12 z-30 w-52 rounded-card border border-line bg-surface-raised p-2 shadow-lg sm:top-10">
                <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[12px] hover:bg-surface-muted" onClick={() => { setShowImportExport(true); setMoreOpen(false); }}>导入 / 导出</button>
                <button type="button" className="min-h-11 w-full rounded-md px-3 text-left text-[12px] hover:bg-surface-muted" onClick={() => void runBatchRetryFailed()} disabled={retrying}>{retrying && <Spinner />} 重试失败{failedCount > 0 ? `（${failedCount}）` : ""}</button>
                <SavedViewMenu filter={advancedFilter} onApply={(filter) => { updateParams({ filter: JSON.stringify(filter) }); setMoreOpen(false); }} onUnauthorized={onUnauthorized} embedded />
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

      <div
        className={cx(
          "grid gap-4",
          !isMobile && !folderCollapsed && "lg:grid-cols-[minmax(190px,240px)_minmax(0,1fr)]",
        )}
      >
        {/* 文件夹树在 390 宽下会把域名列表整个挤到第二屏，收进筛选 sheet；
            桌面上也可以折起来把宽度还给表格。 */}
        {!isMobile && !folderCollapsed && (
          <div className="min-w-0 space-y-2">
            <FolderTree
              selected={folderSelection}
              onSelect={setFolderSelection}
              onDomainDrop={moveDomainsToFolder}
              onUnauthorized={onUnauthorized}
            />
            <button
              type="button"
              className="w-full text-left text-[11px] text-ink-faint hover:text-accent"
              onClick={() => toggleFolderPanel(true)}
            >
              « 收起文件夹
            </button>
          </div>
        )}
        <div className="min-w-0 space-y-4">
          {isMobile ? (
            /* 手机上把七个控件收成一个带计数的「筛选」按钮 + 排序，
               让第一条域名出现在首屏，而不是滚过 1500px 的控件才看得到。 */
            <div className="flex items-center gap-2">
              <button
                type="button"
                className={cx("btn min-h-11 flex-1", filterChips.length > 0 && "border-accent text-accent")}
                onClick={() => setFilterSheetOpen(true)}
                aria-label={filterChips.length > 0 ? `筛选，当前 ${filterChips.length} 个条件` : "筛选"}
              >
                筛选{filterChips.length > 0 ? `（${filterChips.length}）` : ""}
              </button>
              <select
                className="input h-11 w-auto min-w-[7.5rem] shrink-0 py-0 text-[12px]"
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
                className="btn min-h-11 min-w-11 shrink-0 px-0"
                onClick={() => updateParams({ order: order === "asc" ? "desc" : "asc" })}
                aria-label={order === "asc" ? "当前升序，点击改为降序" : "当前降序，点击改为升序"}
              >
                {order === "asc" ? "↑" : "↓"}
              </button>
            </div>
          ) : (
            /* 桌面：一条 flex-wrap 的筛选栏，搜索吃柔性宽，其余控件固定宽。 */
            <div className="card flex flex-wrap items-center gap-2 p-3">
              {filterControls}
              {chipsRow}
            </div>
          )}

          {/* 手机上 chips 放在工具条下面，让人知道当前在筛什么、可以逐个摘掉。 */}
          {isMobile && chipsRow}

          {/* 表格工具条：排序与列显示是表格自身的属性，不该占筛选行的宽度。
              手机上排序已经在上面的工具条里，列设置也没有意义，整条隐藏。 */}
          <div className={cx("relative flex-wrap items-center gap-2", isMobile ? "hidden" : "flex")}>
            {folderCollapsed && (
              <button
                type="button"
                className="btn min-h-9 text-[12px]"
                onClick={() => toggleFolderPanel(false)}
              >
                » 文件夹
              </button>
            )}
            {/* 一整页评分都是「—」时，与其让人对着空列发呆，不如给一个入队入口。
                只入队当前这一页，避免一键把 561 个域名全丢给模型。 */}
            {noScores && (
              <button
                type="button"
                className="btn min-h-9 text-[12px]"
                onClick={() => void runBatchValuation(domains.map((item) => item.name))}
                disabled={valuating}
              >
                {valuating && <Spinner />}为这页 {domains.length} 个域名估价
              </button>
            )}
            <span className="flex-1" />
            <select
              className="input h-9 w-auto min-w-[9.5rem] flex-none py-0 text-[12px]"
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
              className="btn min-h-9 shrink-0"
              onClick={() => updateParams({ order: order === "asc" ? "desc" : "asc" })}
              aria-label={order === "asc" ? "当前升序，点击改为降序" : "当前降序，点击改为升序"}
              title={order === "asc" ? "升序" : "降序"}
            >
              {order === "asc" ? "↑" : "↓"}
            </button>
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
          <button type="button" className="btn min-h-10 text-[12px]" onClick={() => void runBatchValuation()} disabled={valuating}>{valuating && <Spinner />}加入估价</button>
          <button type="button" className="btn min-h-10 text-[12px]" onClick={() => setShowBulkAction(true)}>打标签 / 移入文件夹</button>
          <button type="button" className="btn min-h-10 text-[12px]" onClick={runBatchCheck}>立即检查</button>
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
            <span className="tabular" title={`第 ${data.page} 页，共 ${data.total_pages} 页`}>
              {/* "3 / 12" 说不出你正看着第几条；"101–150 / 561" 才能。 */}
              {(data.page - 1) * data.limit + 1}–{Math.min(data.page * data.limit, data.total_filtered)} / {data.total_filtered}
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

      {/* 移动端筛选 sheet：把整组筛选连同文件夹树一起收进来，
          列表页首屏只留一条工具条。 */}
      {isMobile && filterSheetOpen && (
        <div className="fixed inset-0 z-50 flex flex-col">
          <div
            className="absolute inset-0 bg-overlay/40"
            onClick={() => setFilterSheetOpen(false)}
            role="presentation"
            aria-hidden="true"
          />
          <div
            className="relative mt-auto flex max-h-[88dvh] flex-col rounded-t-card border border-line bg-surface-raised"
            role="dialog"
            aria-modal="true"
            aria-label="筛选域名"
          >
            <header className="flex items-center justify-between gap-2 border-b border-line px-4 py-3">
              <h2 className="text-[14px] text-ink">筛选</h2>
              <button
                type="button"
                className="btn btn-ghost min-h-11 min-w-11 px-0"
                onClick={() => setFilterSheetOpen(false)}
                aria-label="关闭筛选"
              >
                ×
              </button>
            </header>
            <div className="flex-1 space-y-3 overflow-y-auto overscroll-contain p-4">
              {filterControls}
              {chipsRow}
              <FolderTree
                selected={folderSelection}
                onSelect={setFolderSelection}
                onDomainDrop={moveDomainsToFolder}
                onUnauthorized={onUnauthorized}
              />
            </div>
            <footer className="flex items-center gap-2 border-t border-line p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
              <button
                type="button"
                className="btn min-h-11 flex-1"
                onClick={() => setParams(new URLSearchParams(), { replace: true })}
                disabled={filterChips.length === 0}
              >
                清空全部
              </button>
              <button
                type="button"
                className="btn btn-primary min-h-11 flex-1"
                onClick={() => setFilterSheetOpen(false)}
              >
                查看 {folderSelection === "all" ? (data?.total_filtered ?? 0) : domains.length} 个结果
              </button>
            </footer>
          </div>
        </div>
      )}
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
