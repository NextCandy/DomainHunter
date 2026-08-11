import { useCallback, useEffect, useState } from "react";
import { api } from "../lib/api";
import type { Attempt, DomainInfo, Observation } from "../lib/api";
import { Drawer, EmptyState, ErrorNotice, Pill, Spinner, StatusBadge, cx, useToast } from "./ui";
import {
  CONFIDENCE_LABELS,
  formatDateTime,
  formatLatency,
  formatRelative,
  providerLabel,
} from "../lib/format";

interface DetailResponse {
  info: DomainInfo;
  history: Observation[] | null;
  attempts: Attempt[] | null;
}

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
}: {
  domain: string | null;
  onClose: () => void;
  onChanged?: () => void;
  onUnauthorized: () => void;
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
      const result = await api.get<DetailResponse>(`/api/v2/domains/${encodeURIComponent(domain)}`);
      setDetail(result);
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
      await api.post(`/api/v2/domains/${encodeURIComponent(domain)}/check`);
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

  const info = detail?.info;

  return (
    <Drawer
      open={Boolean(domain)}
      onClose={onClose}
      title={<span className="mono text-[14px]">{domain}</span>}
      subtitle={
        info && (
          <span className="flex flex-wrap items-center gap-2">
            <StatusBadge status={info.status} />
            {info.confidence && <Pill>可信度 {CONFIDENCE_LABELS[info.confidence] ?? info.confidence}</Pill>}
            <Pill>{providerLabel(info.query_method)}</Pill>
          </span>
        )
      }
    >
      {loading && !detail && (
        <div className="flex items-center gap-2 text-ink-muted">
          <Spinner /> 加载中…
        </div>
      )}
      {error && <ErrorNotice message={error} onRetry={load} />}

      {info && (
        <>
          <div className="mb-4 flex flex-wrap gap-2">
            <button type="button" className="btn btn-primary h-8" onClick={runCheck} disabled={checking}>
              {checking && <Spinner />}立即检查
            </button>
            <button type="button" className="btn h-8" onClick={toggleFavorite}>
              {info.favorite ? "取消收藏" : "加入观察列表"}
            </button>
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

          {tab === "overview" && <OverviewTab info={info} />}
          {tab === "evidence" && <EvidenceTab info={info} attempts={detail?.attempts ?? null} />}
          {tab === "timeline" && <TimelineTab history={detail?.history ?? null} />}
          {tab === "raw" && <RawTab info={info} />}
        </>
      )}
    </Drawer>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-line py-2 last:border-0">
      <span className="shrink-0 text-[12px] text-ink-muted">{label}</span>
      <span className="min-w-0 break-all text-right text-[13px]">{children}</span>
    </div>
  );
}

function OverviewTab({ info }: { info: DomainInfo }) {
  return (
    <div>
      <Field label="状态">
        <StatusBadge status={info.status} />
      </Field>
      <Field label="注册商">{info.registrar || "—"}</Field>
      <Field label="注册时间">{formatDateTime(info.created_date)}</Field>
      <Field label="更新时间">{formatDateTime(info.updated_date)}</Field>
      <Field label="到期时间">{formatDateTime(info.expiry_date)}</Field>
      <Field label="名称服务器">
        {info.name_servers && info.name_servers.length > 0 ? (
          <span className="mono block whitespace-pre-line">{info.name_servers.join("\n")}</span>
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
      <Field label="最后查询">
        {formatDateTime(info.last_checked)}
        <span className="ml-1 text-[12px] text-ink-faint">({formatRelative(info.last_checked)})</span>
      </Field>
      <Field label="下次查询">
        {formatDateTime(info.next_check_at)}
        <span className="ml-1 text-[12px] text-ink-faint">({formatRelative(info.next_check_at)})</span>
      </Field>
      <Field label="加入时间">{formatDateTime(info.added_at)}</Field>
      {info.error_message && (
        <Field label="备注">
          <span className="text-amber-700 dark:text-amber-400">{info.error_message}</span>
        </Field>
      )}
    </div>
  );
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
                        <span className="text-red-600 dark:text-red-400" title={attempt.error_message}>
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
  return (
    <ol className="relative space-y-3 border-l border-line pl-4">
      {history.map((item) => (
        <li key={item.id} className="relative">
          <span
            className={cx(
              "absolute -left-[21px] top-1.5 h-2 w-2 rounded-full",
              item.changed ? "bg-accent" : "bg-line",
            )}
          />
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge status={item.status} />
            {item.changed && <Pill className="text-accent">状态变化</Pill>}
            <span className="text-[12px] text-ink-faint">{formatDateTime(item.observed_at)}</span>
          </div>
          <p className="mt-0.5 text-[12px] text-ink-muted">
            来源 {providerLabel(item.provider)}
            {item.registrar ? ` · ${item.registrar}` : ""}
          </p>
        </li>
      ))}
    </ol>
  );
}

function RawTab({ info }: { info: DomainInfo }) {
  if (!info.whois_raw) {
    return <EmptyState title="暂无原始报文" hint="等待查询完成后再试" />;
  }
  return (
    <pre className="mono max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-surface-muted p-3 leading-relaxed">
      {info.whois_raw}
    </pre>
  );
}
