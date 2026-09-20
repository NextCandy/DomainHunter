import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../lib/api";
import { ApiError } from "../lib/api";
import type { Attempt, DomainInfo, DomainListResult, DomainStatus, Observation } from "../lib/api";
import { useAsync } from "../lib/useAsync";
import {
  Drawer,
  EmptyState,
  ErrorNotice,
  Pill,
  Skeleton,
  Spinner,
  FromToStatus,
  StatusBadge,
  cx,
  useToast,
} from "./ui";
import {
  CONFIDENCE_LABELS,
  formatDate,
  formatDateTime,
  formatExpiryRelative,
  formatLatency,
  formatRelative,
  providerLabel,
} from "../lib/format";
import { RegistrarLookupMenu } from "./RegistrarLookupMenu";
import { DomainValuationPanel } from "../features/ai-valuation";

interface DetailResponse {
  info: DomainInfo;
  history?: Observation[] | null;
  attempts?: Attempt[] | null;
}

/* 时间线节点同样零新色相：唯一的陶土只落在「可注册 / 待删除」这两个
   需要立刻行动的节点上，其余用墨色到暖中性的明度阶梯。 */
const TIMELINE_COLORS: Record<DomainStatus, string> = {
  available: "rgb(var(--action))",
  registered: "rgb(var(--neutral))",
  grace: "rgb(var(--warning))",
  redemption: "rgb(var(--info))",
  pending_delete: "rgb(var(--action))",
  expired: "rgb(var(--warning))",
  transfer_locked: "rgb(var(--info))",
  hold: "rgb(var(--info))",
  unknown: "rgb(var(--neutral))",
  error: "rgb(var(--danger))",
  skipped: "rgb(var(--neutral))",
};

type Tab = "overview" | "evidence" | "timeline" | "raw";

const TABS: Array<{ id: Tab; label: string }> = [
  { id: "overview", label: "概览" },
  { id: "evidence", label: "查询证据" },
  { id: "timeline", label: "状态时间线" },
  { id: "raw", label: "原始报文" },
];

export function DomainDrawer({
  domain,
  onClose,
  onChanged,
  onUnauthorized,
  domains,
  onNavigate,
  variant = "drawer",
}: {
  domain: string | null;
  onClose: () => void;
  onChanged?: () => void;
  onUnauthorized: () => void;
  domains?: string[];
  onNavigate?: (domain: string) => void;
  /**
   * "drawer" 是默认形态；"page" 去掉抽屉外壳，用同一份内容铺满
   * /domains/:name 这个可分享的深链页面——同一个组件，两种容器。
   */
  variant?: "drawer" | "page";
}) {
  const [detail, setDetail] = useState<DetailResponse | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [checking, setChecking] = useState(false);
  const [tab, setTab] = useState<Tab>("overview");
  const toast = useToast();

  const load = useCallback(async () => {
    if (!domain) return;
    setLoading(true);
    setError("");
    try {
      const encoded = encodeURIComponent(domain);
      const result = await api.getOptional<DetailResponse>(`/api/v2/domains/${encoded}`);
      if (result?.info) {
        setDetail({
          info: result.info,
          history: result.history ?? [],
          attempts: result.attempts ?? [],
        });
      } else {
        // 旧后端没有 v2 详情时，至少展示当前快照；历史/证据留空而不是让抽屉崩溃。
        const legacy = await api.getOptional<DomainInfo>(`/api/domain/${encoded}`);
        if (!legacy) throw new Error("当前后端暂不支持域名详情");
        setDetail({ info: legacy, history: [], attempts: [] });
      }
    } catch (err) {
      if (err instanceof Error && err.message.includes("会话")) onUnauthorized();
      setError(err instanceof Error ? err.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, [domain, onUnauthorized]);

  useEffect(() => {
    setTab("overview");
    setDetail(null);
    void load();
  }, [load]);

  async function runCheck() {
    if (!domain) return;
    setChecking(true);
    try {
      const encoded = encodeURIComponent(domain);
      try {
        await api.post(`/api/v2/domains/${encoded}/check`);
      } catch (err) {
        if (!(err instanceof ApiError) || err.status !== 404) throw err;
        await api.post(`/api/domain/check/${encoded}`);
      }
      toast("查询完成", "success");
      await load();
      onChanged?.();
    } catch (err) {
      toast(err instanceof Error ? err.message : "查询失败", "error");
    } finally {
      setChecking(false);
    }
  }

  async function toggleFavorite() {
    if (!domain || !detail) return;
    try {
      const updated = await api.patch<DomainInfo>(`/api/v2/domains/${encodeURIComponent(domain)}`, {
        favorite: !detail.info.favorite,
      });
      setDetail({ ...detail, info: updated });
      onChanged?.();
    } catch (err) {
      toast(err instanceof Error ? err.message : "操作失败", "error");
    }
  }

  async function toggleNotify() {
    if (!domain || !detail) return;
    const next = detail.info.notify === false;
    try {
      const updated = await api.patch<DomainInfo>(`/api/v2/domains/${encodeURIComponent(domain)}`, { notify: next });
      setDetail({ ...detail, info: updated });
      toast(next ? "已恢复该域名的到期与状态提醒" : "已忽略该域名的后续提醒", "success");
      onChanged?.();
    } catch (err) {
      toast(err instanceof Error ? err.message : "更新提醒设置失败", "error");
    }
  }

  const info = detail?.info;
  const navigation = useMemo(() => {
    const index = domain && domains ? domains.indexOf(domain) : -1;
    return {
      previous: index > 0 ? domains?.[index - 1] : undefined,
      next: index >= 0 && index < (domains?.length ?? 0) - 1 ? domains?.[index + 1] : undefined,
      // 翻了半天不知道翻到哪了，补一个"第几 / 共几"。
      position: index >= 0 ? index + 1 : 0,
      total: domains?.length ?? 0,
    };
  }, [domain, domains]);

  const heading = <span className="mono text-[14px]">{domain}</span>;
  const meta = (
    info && (
      <span className="flex flex-wrap items-center gap-2">
        <StatusBadge status={info.status} eppStatuses={info.epp_statuses} />
        {info.confidence && <Pill>可信度 {CONFIDENCE_LABELS[info.confidence] ?? info.confidence}</Pill>}
        {info.review?.required && <span className="review-badge">数据需复核</span>}
        <Pill>{providerLabel(info.query_method)}</Pill>
        {info.cached && <Pill title="本次结果来自查询缓存">cached: true</Pill>}
      </span>
    )
  );

  const body = (
    <>
      {loading && !detail && <DrawerSkeleton />}
      {loading && detail && (
        <div className="mb-3 flex items-center gap-2 text-[12px] text-ink-muted">
          <Spinner /> 正在更新详情…
        </div>
      )}
      {error && <ErrorNotice message={error} onRetry={load} />}

      {info && (
        <>
          {(navigation.previous || navigation.next) && (
            <div className="mb-3 flex items-center justify-between gap-2">
              <button type="button" className="btn min-h-10" disabled={!navigation.previous} onClick={() => navigation.previous && onNavigate?.(navigation.previous)}>← 上一条</button>
              {navigation.total > 0 && (
                <span className="tabular text-[12px] text-ink-faint">
                  {navigation.position} / {navigation.total}
                </span>
              )}
              <button type="button" className="btn min-h-10" disabled={!navigation.next} onClick={() => navigation.next && onNavigate?.(navigation.next)}>下一条 →</button>
            </div>
          )}
          <div className="mb-4 flex flex-wrap gap-2">
            <button type="button" className="btn btn-primary h-8" onClick={runCheck} disabled={checking}>
              {checking && <Spinner />}立即检查
            </button>
            <button type="button" className="btn h-8" onClick={toggleFavorite}>
              {info.favorite ? "取消收藏" : "加入观察列表"}
            </button>
            {info.status === "available" && <RegistrarLookupMenu domain={info.name} />}
            {/* 这个域名以前是干什么的——存档站是最快的答案，外链标明是站外。 */}
            <a
              className="btn h-8"
              href={`https://web.archive.org/web/*/${encodeURIComponent(info.name)}`}
              target="_blank"
              rel="noopener noreferrer"
              title="在 Wayback Machine 查看历史快照（外部站点）"
            >
              Wayback <span aria-hidden="true">↗</span>
            </a>
            {/* 抽屉分享不出去。全屏页是同一个组件、同一份内容，只是换了容器。 */}
            {variant === "drawer" && (
              <Link to={`/domains/${encodeURIComponent(info.name)}`} className="btn h-8" title="在新页打开，链接可分享">
                在新页打开 <span aria-hidden="true">↗</span>
              </Link>
            )}
          </div>

          <nav className="mb-3 flex gap-1 border-b border-line">
            {TABS.map((item) => (
              <button
                key={item.id}
                type="button"
                onClick={() => setTab(item.id)}
                className={cx(
                  "-mb-px border-b-2 px-2.5 py-1.5 text-[13px] transition-colors",
                  tab === item.id
                    ? "border-accent font-medium text-ink"
                    : "border-transparent text-ink-muted hover:text-ink",
                )}
              >
                {item.label}
              </button>
            ))}
          </nav>

          {tab === "overview" && (
            <OverviewTab
              info={info}
              onUnauthorized={onUnauthorized}
              onToggleNotify={toggleNotify}
              onCompleted={() => {
                onChanged?.();
                void load();
              }}
            />
          )}
          {tab === "evidence" && <EvidenceTab info={info} attempts={detail?.attempts ?? null} />}
          {tab === "timeline" && <TimelineTab history={detail?.history ?? null} />}
          {tab === "raw" && <RawTab info={info} />}
        </>
      )}
    </>
  );

  if (variant === "page") {
    return (
      <div className="space-y-4">
        <header className="workspace-header">
          <span className="workspace-kicker">DOMAIN DETAIL</span>
          {/* 全屏页上域名是主角，给它比抽屉里更大的字号。 */}
          <h1 className="mono mt-1 break-all text-[24px]">{domain}</h1>
          <div className="mt-2">{meta}</div>
        </header>
        <div className="card p-6">{body}</div>
      </div>
    );
  }

  return (
    <Drawer open={Boolean(domain)} onClose={onClose} title={heading} subtitle={meta}>
      {body}
    </Drawer>
  );
}

function Field({ label, children, align = "right" }: { label: string; children: React.ReactNode; align?: "right" | "left" }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-line py-2 last:border-0">
      <span className="shrink-0 text-[12px] text-ink-muted">{label}</span>
      <span className={cx("min-w-0 break-words text-[13px]", align === "right" ? "text-right" : "text-left")}>{children}</span>
    </div>
  );
}

/** 注册至今的年数，用于把 1997-02-14 这种日期翻译成「已注册 29 年」。 */
function registeredForLabel(value?: string | null): string {
  if (!value) return "";
  const start = new Date(value);
  if (Number.isNaN(start.getTime())) return "";
  const years = (Date.now() - start.getTime()) / (365.2425 * 86_400_000);
  if (years < 1) return `${Math.max(1, Math.round(years * 12))} 个月`;
  return `${Math.floor(years)} 年`;
}

function OverviewTab({ info, onUnauthorized, onToggleNotify, onCompleted }: { info: DomainInfo; onUnauthorized: () => void; onToggleNotify: () => void; onCompleted: () => void }) {
  const evidence = info.evidence ?? [];
  const statuses = new Set(evidence.filter((item) => item.status !== "unknown" && item.status !== "skipped").map((item) => item.status));
  const consistent = statuses.size <= 1;
  const winner = evidence.find((item) => item.provider === info.query_method) ?? evidence.find((item) => item.status === info.status);
  return (
    <div className="space-y-4">
      <section className={cx("rounded-card border border-l-4 p-4", consistent ? "border-l-success" : "border-l-warning")}>
        <div className="flex flex-wrap items-center gap-2"><StatusBadge status={info.status} eppStatuses={info.epp_statuses} /><Pill>可信度 {CONFIDENCE_LABELS[info.confidence ?? ""] ?? info.confidence ?? "—"}</Pill><Pill className={consistent ? "text-success" : "text-warning"}>{consistent ? "多源一致" : "多源有冲突"}</Pill></div>
        <p className="mt-2 text-[12px] text-ink-muted">{consistent ? `有效查询源结论一致，采纳 ${providerLabel(info.query_method)}。` : `按权威性与证据完整度采纳 ${providerLabel(info.query_method)}；RDAP/注册局结构化证据优先于 WHOIS 文本解析。`}</p>
        {winner?.note && <p className="mt-1 text-[11px] text-ink-faint">采纳说明：{winner.note}</p>}
      </section>
      {info.review?.required && (
        <div className="rounded-md border border-review/35 bg-review/8 px-3 py-2 text-[12px] leading-5">
          <div className="font-semibold text-review">数据需复核</div>
          <p className="mt-0.5 text-ink-muted">{info.review.explanation ?? "请先查看查询证据并重新检查；这不是新的域名生命周期状态。"}</p>
        </div>
      )}
      {/* 到期与提醒原来挤在定义列表最末尾，是这一页最该先看到的事实之一。
          单独提成一块，配一个真正能点的按钮。 */}
      <section className="rounded-card border border-line p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-[12px] text-ink-muted">到期时间</p>
            <p className="mt-1 text-[18px] text-ink" title={formatDateTime(info.expiry_date)}>
              {formatExpiryRelative(info.expiry_date)}
            </p>
            <p className="tabular mt-0.5 text-[11px] text-ink-faint">{formatDate(info.expiry_date)}</p>
          </div>
          <div className="text-right">
            <p className="text-[12px] text-ink-muted">到期与状态提醒</p>
            <button type="button" className="btn mt-1.5 min-h-10" onClick={() => void onToggleNotify()}>
              {info.notify === false ? "恢复提醒" : "忽略此域名"}
            </button>
          </div>
        </div>
      </section>

      {/* 关键事实排成 2×N 网格：一列长长的定义列表得上下扫，
          两列并排一眼能对比"注册了多久 / 还剩多久"。 */}
      <section className="rounded-card border border-line px-4">
        <h3 className="border-b border-line py-3 text-[13px] font-medium">关键事实</h3>
        <div className="grid gap-x-6 sm:grid-cols-2">
      {info.cached && <Field label="缓存">cached: true</Field>}
      <Field label="注册商">{info.registrar || "—"}</Field>
      <Field label="注册时间" >
        {formatDate(info.created_date)}
        {/* 1997 年注册的域名和去年注册的完全是两种资产，把年数直接算出来。 */}
        {registeredForLabel(info.created_date) && (
          <span className="ml-1 text-[12px] text-ink-faint">（{registeredForLabel(info.created_date)}）</span>
        )}
      </Field>
      <Field label="更新时间">{formatDate(info.updated_date)}</Field>
      <Field label="最后查询">{formatRelative(info.last_checked)}</Field>
      <Field label="下次查询">{formatRelative(info.next_check_at)}</Field>
      <Field label="加入时间">{formatDate(info.added_at)}</Field>
        </div>
      <Field label="名称服务器" align="left">
        {info.name_servers && info.name_servers.length > 0 ? (
          <span className="mono block whitespace-pre-line text-left">{info.name_servers.join("\n")}</span>
        ) : (
          "—"
        )}
      </Field>
      {info.epp_statuses && info.epp_statuses.length > 0 && (
        <Field label="EPP 状态">
          <span className="flex flex-wrap justify-end gap-1">
            {info.epp_statuses.map((status) => (
              <Pill key={status}>{status}</Pill>
            ))}
          </span>
        </Field>
      )}
      {info.error_message && (
        <Field label="备注">
          <span className="text-ink-muted">{info.error_message}</span>
        </Field>
      )}
      </section>
      <DomainValuationPanel domain={info.name} onUnauthorized={onUnauthorized} onCompleted={onCompleted} compact />
      <SimilarDomains info={info} onUnauthorized={onUnauthorized} />
      <ConsistencyTable info={info} />
    </div>
  );
}

/**
 * 同名不同后缀的域名。
 *
 * 判断一个 SLD 值不值，很大程度看它在别的后缀下是什么情况——
 * digitalgarden.com 旁边如果 .net 也在清单里且是可注册，那是完全不同的决策。
 * 数据直接用列表接口按 SLD 搜，不新造 API。
 */
function SimilarDomains({ info, onUnauthorized }: { info: DomainInfo; onUnauthorized: () => void }) {
  const sld = info.name.split(".")[0] ?? "";
  const { data } = useAsync<DomainListResult>(
    () => api.get<DomainListResult>(`/api/v2/domains?search=${encodeURIComponent(sld)}&limit=50`),
    [sld],
    onUnauthorized,
  );

  const siblings = (data?.domains ?? []).filter(
    (item) => item.name !== info.name && (item.name.split(".")[0] ?? "") === sld,
  );
  if (siblings.length === 0) return null;

  return (
    <section className="rounded-card border border-line p-4">
      <h3 className="text-[13px] font-medium">同名其它后缀（{siblings.length}）</h3>
      <ul className="mt-2 flex flex-wrap gap-2">
        {siblings.map((item) => (
          <li key={item.name}>
            <Link
              to={`/domains/${encodeURIComponent(item.name)}`}
              className="inline-flex items-center gap-1.5 rounded-tag border border-line px-2 py-1 text-[11px] hover:border-accent"
            >
              <span className="mono">{item.name}</span>
              <StatusBadge status={item.status} />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

function ConsistencyTable({ info }: { info: DomainInfo }) {
  const evidence = info.evidence ?? [];
  if (evidence.length === 0) return <section className="rounded-card border border-line"><EmptyState title="暂无多源裁决数据" hint="重新检查后会展示各源结论" /></section>;
  const conflicting = new Set(evidence.map((item) => item.status)).size > 1;
  return <section className={cx("rounded-card border", conflicting ? "border-warning/40" : "border-line")}><h3 className="border-b border-line px-4 py-3 text-[13px] font-medium">多源一致性裁决</h3><div className="table-scroll"><table className="w-full min-w-[480px] text-left text-[11px]"><thead className="bg-surface-muted text-ink-muted"><tr><th className="px-3 py-2">源</th><th className="px-3 py-2">结论</th><th className="px-3 py-2">可信度</th><th className="px-3 py-2">耗时</th><th className="px-3 py-2">时间</th></tr></thead><tbody className="divide-y divide-line">{evidence.map((item, index) => <tr key={`${item.provider}-${index}`} className={item.provider === info.query_method ? "bg-accent-soft/30" : ""}><td className="px-3 py-2">{providerLabel(item.provider)}</td><td className="px-3 py-2"><StatusBadge status={item.status} /></td><td className="px-3 py-2">{CONFIDENCE_LABELS[item.confidence] ?? item.confidence}</td><td className="px-3 py-2">{formatLatency(item.latency_ms)}</td><td className="px-3 py-2">{formatDateTime(item.queried_at)}</td></tr>)}</tbody></table></div><p className="border-t border-line px-4 py-3 text-[11px] text-ink-muted">{conflicting ? "存在冲突：结构化 RDAP 或注册局证据优先；最终采用行已高亮。" : "有效来源结论一致；最终采用行已高亮。"}</p></section>;
}

function EvidenceTab({ info, attempts }: { info: DomainInfo; attempts: Attempt[] | null }) {
  const evidence = info.evidence ?? [];
  return (
    <div className="space-y-4">
      <section>
        <h3 className="mb-2 text-[12px] font-medium text-ink-muted">本次查询证据</h3>
        {evidence.length === 0 ? (
          <EmptyState title="本次结果没有随附证据" hint="重新执行一次查询即可看到各查询源的结论" />
        ) : (
          <ul className="divide-y divide-line rounded-md border border-line">
            {evidence.map((item, index) => (
              <li key={`${item.provider}-${index}`} className="flex items-center gap-2 px-3 py-2">
                <span className="w-[92px] shrink-0 text-[13px]">{providerLabel(item.provider)}</span>
                <StatusBadge status={item.status} />
                <span className="tabular ml-auto text-[12px] text-ink-faint">
                  {formatLatency(item.latency_ms)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section>
        <h3 className="mb-2 text-[12px] font-medium text-ink-muted">历史查询尝试</h3>
        {!attempts || attempts.length === 0 ? (
          <EmptyState title="暂无查询尝试记录" />
        ) : (
          <div className="table-scroll rounded-md border border-line">
            <table className="w-full min-w-[420px] text-left">
              <thead className="bg-surface-muted text-[11px] text-ink-muted">
                <tr>
                  <th className="px-3 py-1.5 font-medium">时间</th>
                  <th className="px-3 py-1.5 font-medium">查询源</th>
                  <th className="px-3 py-1.5 font-medium">结果</th>
                  <th className="px-3 py-1.5 text-right font-medium">耗时</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line text-[12px]">
                {attempts.map((attempt) => (
                  <tr key={attempt.id}>
                    <td className="tabular whitespace-nowrap px-3 py-1.5 text-ink-muted">
                      {formatDateTime(attempt.queried_at)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5">{providerLabel(attempt.provider)}</td>
                    <td className="px-3 py-1.5">
                      {attempt.success ? (
                        <StatusBadge status={attempt.status} />
                      ) : (
                        <span className="text-ink-muted" title={attempt.error_message}>
                          {attempt.error_message || "失败"}
                        </span>
                      )}
                    </td>
                    <td className="tabular whitespace-nowrap px-3 py-1.5 text-right text-ink-faint">
                      {formatLatency(attempt.latency_ms)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function TimelineTab({ history }: { history: Observation[] | null }) {
  if (!history || history.length === 0) {
    return <EmptyState title="暂无观测历史" hint="下一次查询完成后就会出现记录" />;
  }

  const events = [...history].reverse();
  const width = Math.max(320, events.length * 72);
  const points = events.map((_, index) => {
    const x = events.length === 1 ? width / 2 : 24 + (index / (events.length - 1)) * (width - 48);
    return { x, y: 32 };
  });
  const path = points.map((point, index) => `${index === 0 ? "M" : "L"} ${point.x} ${point.y}`).join(" ");

  return (
    <div className="space-y-4">
      <div className="table-scroll rounded-md border border-line bg-surface-muted/40 p-2">
        <svg
          className="h-20 w-full"
          style={{ minWidth: `${width}px` }}
          viewBox={`0 0 ${width} 64`}
          role="img"
          aria-label="域名状态变化时间线"
          preserveAspectRatio="none"
        >
          <path d={path} fill="none" stroke="rgb(var(--line))" strokeWidth="2" />
          {events.map((item, index) => {
            const point = points[index];
            const color = TIMELINE_COLORS[item.status] ?? "rgb(var(--ink-faint))";
            return (
              <Fragment key={item.id}>
                <circle
                  cx={point.x}
                  cy={point.y}
                  r={item.changed ? 6 : 4.5}
                  fill={color}
                  stroke="rgb(var(--surface-raised))"
                  strokeWidth="2"
                >
                  <title>
                    {STATUS_LABELS_SAFE[item.status] ?? item.status} · {formatDateTime(item.observed_at)}
                  </title>
                </circle>
              </Fragment>
            );
          })}
        </svg>
      </div>

      <ol className="relative space-y-3 border-l border-line pl-4">
        {history.map((item, index) => {
          // history 是倒序（新→旧），所以"上一次观测"在数组里的下一个位置。
          const previous = history[index + 1];
          const changes = previous ? diffObservation(previous, item) : [];
          return (
            <li key={item.id} className="relative">
              <span
                className={cx(
                  "absolute -left-[21px] top-1.5 h-2 w-2 rounded-full",
                  item.changed ? "bg-accent" : "bg-line",
                )}
              />
              <div className="flex flex-wrap items-center gap-2">
                <FromToStatus from={previous?.status} to={item.status} />
                {item.changed && <Pill className="text-accent">状态变化</Pill>}
                <span className="text-[12px] text-ink-faint">{formatDateTime(item.observed_at)}</span>
              </div>
              <p className="mt-0.5 text-[12px] text-ink-muted">
                来源 {providerLabel(item.provider)}
              </p>
              {/* 字段级 diff：状态没变但到期日被续了一年，同样是重要信息。 */}
              {changes.length > 0 && (
                <ul className="mt-1.5 space-y-1 rounded-md border border-warn/35 bg-warn/8 px-2.5 py-1.5">
                  {changes.map((change) => (
                    <li key={change.label} className="text-[11px] leading-4">
                      <span className="text-ink-muted">{change.label}</span>{" "}
                      <span className="text-ink-faint line-through">{change.from}</span>{" "}
                      <span aria-hidden="true">→</span>{" "}
                      <span className="text-ink">{change.to}</span>
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
}

/**
 * 相邻两次观测之间的字段变化。
 *
 * 只比 observation 表里真实存了的三个字段：注册商、到期日、名称服务器。
 * EPP 状态没有随观测入库（只在当前快照 DomainInfo 上），所以这里不做 EPP 的
 * diff——宁可少一项，也不能拿当前值冒充历史值。
 * TODO: 若要支持 EPP / DNSSEC 的历史 diff，需要 domain_observations 增列
 * epp_statuses 并在 Save 时一起写入。
 */
function diffObservation(from: Observation, to: Observation): Array<{ label: string; from: string; to: string }> {
  const out: Array<{ label: string; from: string; to: string }> = [];
  const day = (value?: string | null) => (value ? formatDate(value) : "—");
  const ns = (value?: string[] | null) => (value && value.length > 0 ? [...value].sort().join(", ") : "—");

  if ((from.registrar || "") !== (to.registrar || "")) {
    out.push({ label: "注册商", from: from.registrar || "—", to: to.registrar || "—" });
  }
  if (day(from.expiry_at) !== day(to.expiry_at)) {
    out.push({ label: "到期日", from: day(from.expiry_at), to: day(to.expiry_at) });
  }
  if (ns(from.name_servers) !== ns(to.name_servers)) {
    out.push({ label: "名称服务器", from: ns(from.name_servers), to: ns(to.name_servers) });
  }
  return out;
}

function RawTab({ info }: { info: DomainInfo }) {
  if (!info.whois_raw) {
    return <EmptyState title="暂无原始报文" hint="等待查询完成后再试" />;
  }
  const lines = info.whois_raw.replace(/\r\n?/g, "\n").split("\n");
  return (
    <div className="raw-code" role="region" aria-label="原始报文（带行号）">
      {lines.map((line, index) => (
        <Fragment key={index}>
          <span className="raw-line-number" aria-hidden="true">
            {index + 1}
          </span>
          <pre className="raw-line">{line || " "}</pre>
        </Fragment>
      ))}
    </div>
  );
}

function DrawerSkeleton() {
  return (
    <div className="space-y-4" role="status" aria-label="正在加载域名详情">
      <div className="flex items-center gap-2">
        <Skeleton className="h-8 w-24" />
        <Skeleton className="h-8 w-28" />
      </div>
      <div className="flex gap-3 border-b border-line pb-2">
        <Skeleton className="h-5 w-12" />
        <Skeleton className="h-5 w-16" />
        <Skeleton className="h-5 w-20" />
      </div>
      <div className="space-y-3">
        {Array.from({ length: 7 }, (_, index) => (
          <div key={index} className="flex items-center justify-between gap-4">
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-3 w-40" />
          </div>
        ))}
      </div>
    </div>
  );
}

const STATUS_LABELS_SAFE: Partial<Record<DomainStatus, string>> = {
  available: "可注册",
  registered: "已注册",
  grace: "宽限期",
  redemption: "赎回期",
  pending_delete: "待删除",
  expired: "已过期",
  transfer_locked: "转移锁定",
  hold: "Hold",
  unknown: "未知",
  error: "查询失败",
  skipped: "已跳过",
};
