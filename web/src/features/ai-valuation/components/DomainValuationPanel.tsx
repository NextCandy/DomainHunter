import { useState } from "react";
import "../valuation.css";
import type { DomainStatus } from "../../../lib/api";
import { formatDateTime, formatRelative } from "../../../lib/format";
import {
  ErrorNotice,
  Pill,
  Spinner,
  cx,
  useToast,
} from "../../../components/ui";
import { useDomainValuation } from "../hooks/useDomainValuation";
import {
  formatCNY,
  VALUATION_CONFIDENCE_LABELS,
  VALUATION_RISK_LABELS,
  VALUATION_STATE_LABELS,
  type DomainValuation,
  type ValuationJob,
} from "../lib/valuation-types";

export interface DomainValuationPanelProps {
  domain: string;
  status: DomainStatus;
  confidence?: string;
  reviewRequired?: boolean;
  defaultProfileId?: string;
  onUnauthorized?: () => void;
  onCompleted?: () => void;
  compact?: boolean;
}

/**
 * 放在 DomainDrawer「概览」标签页的顶部，或作为独立的详情侧栏模块。
 * 不把 whois_raw、用户备注、注册人资料或任何密钥送到浏览器之外。
 */
export function DomainValuationPanel({
  domain,
  status,
  confidence,
  reviewRequired = false,
  defaultProfileId,
  onUnauthorized,
  onCompleted,
  compact = false,
}: DomainValuationPanelProps) {
  const toast = useToast();
  const [showDetails, setShowDetails] = useState(!compact);
  const {
    job,
    loading,
    submitting,
    error,
    enqueue,
    cancel,
    retry,
    refresh,
    clearError,
  } = useDomainValuation(domain, {
    onUnauthorized,
    onCompleted: (completed) => {
      if (completed.state === "succeeded") toast("AI 估价已完成", "success");
      else if (completed.state === "failed")
        toast("AI 估价未完成，请查看失败原因", "error");
      onCompleted?.();
    },
  });

  const hasReviewConstraint =
    reviewRequired ||
    confidence === "low" ||
    status === "error" ||
    status === "unknown" ||
    status === "skipped";
  const canEnqueue =
    !hasReviewConstraint &&
    (!job || ["idle", "failed", "cancelled", "deferred"].includes(job.state));

  async function startValuation(forceRefresh = false) {
    const created = await enqueue({
      profile_id: defaultProfileId,
      priority: "normal",
      force_refresh: forceRefresh,
    });
    if (created)
      toast(
        created.cached ? "已命中可用的估价缓存" : "已加入 AI 估价队列",
        "success",
      );
  }

  return (
    <section className="ai-valuation-panel">
      <header className="flex items-start justify-between gap-3 border-b border-line bg-accent-soft/45 px-4 py-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <span className="mono text-[11px] font-semibold tracking-[0.12em] text-accent">
              AI RESEARCH
            </span>
            {job && <JobStatePill state={job.state} />}
          </div>
          <h3 className="mt-1 text-[14px] font-semibold text-ink">
            AI 域名鉴定报告
          </h3>
          <p className="mt-1 text-[12px] leading-5 text-ink-muted">
            默认使用当前启用的 AI
            配置；输出评分、人民币价格区间和用途分析，不改变查询状态或可注册结论。
          </p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          className="btn h-8 shrink-0 text-[12px]"
          disabled={loading || submitting}
          aria-label="刷新 AI 估价状态"
        >
          {loading ? <Spinner /> : "刷新"}
        </button>
      </header>

      <div className="p-4">
        {error && (
          <ErrorNotice
            message={error}
            onRetry={() => {
              clearError();
              void refresh();
            }}
          />
        )}

        {hasReviewConstraint && (
          <div className="mb-3 rounded-card border border-line bg-surface-muted px-3 py-2.5 text-[12px] leading-5 text-ink">
            <div className="font-semibold text-ink">需要先复核查询事实</div>
            <p className="mt-1 text-ink-muted">
              当前状态、可信度或查询结果不足以支撑研究性估价。请先完成“立即检查”并在证据一致后入队。
            </p>
          </div>
        )}

        {loading && !job && <LoadingSkeleton />}
        {!loading && !job && (
          <EmptyValuationState
            disabled={!canEnqueue || submitting}
            reviewRequired={hasReviewConstraint}
            onStart={() => void startValuation(false)}
          />
        )}
        {job && (
          <ValuationJobContent
            job={job}
            compact={compact}
            showDetails={showDetails}
            onToggleDetails={() => setShowDetails((value) => !value)}
            onStart={startValuation}
            onRetry={retry}
            onCancel={cancel}
            pending={submitting}
            allowRetry={Boolean(canEnqueue)}
          />
        )}
      </div>
    </section>
  );
}

function JobStatePill({ state }: { state: ValuationJob["state"] }) {
  const classes: Record<ValuationJob["state"], string> = {
    idle: "border-line bg-surface-muted text-ink-muted",
    queued: "border-accent/30 bg-accent-soft text-accent",
    running: "border-accent/30 bg-accent-soft text-accent",
    succeeded: "border-accent/30 bg-accent-soft text-accent",
    failed: "border-line bg-surface-muted text-ink-muted",
    cancelled: "border-line bg-surface-muted text-ink-muted",
    deferred: "border-line bg-surface-muted text-ink-muted",
  };
  return (
    <Pill className={cx("border", classes[state])}>
      {VALUATION_STATE_LABELS[state]}
    </Pill>
  );
}

function EmptyValuationState({
  disabled,
  reviewRequired,
  onStart,
}: {
  disabled: boolean;
  reviewRequired: boolean;
  onStart: () => void;
}) {
  return (
    <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
      <div>
        <p className="text-[13px] font-medium text-ink">尚无估价记录</p>
        <p className="mt-1 text-[12px] leading-5 text-ink-muted">
          默认使用已启用的 AI 配置档案。可在 AI
          与自动化中更换提供商、模型、并发和安全的 Base URL。
        </p>
      </div>
      <button
        type="button"
        className="btn btn-primary h-9 whitespace-nowrap"
        disabled={disabled}
        onClick={onStart}
      >
        加入估价队列
      </button>
      {reviewRequired && (
        <p className="sm:col-span-2 text-[11px] text-ink-muted">
          该操作已因数据复核状态安全禁用。
        </p>
      )}
    </div>
  );
}

function ValuationJobContent({
  job,
  compact,
  showDetails,
  onToggleDetails,
  onStart,
  onRetry,
  onCancel,
  pending,
  allowRetry,
}: {
  job: ValuationJob;
  compact: boolean;
  showDetails: boolean;
  onToggleDetails: () => void;
  onStart: (forceRefresh?: boolean) => Promise<void>;
  onRetry: () => Promise<void>;
  onCancel: () => Promise<void>;
  pending: boolean;
  allowRetry: boolean;
}) {
  if (job.state === "deferred") {
    return (
      <DeferredState
        job={job}
        pending={pending}
        allowRetry={allowRetry}
        onRetry={onRetry}
        onCancel={onCancel}
      />
    );
  }

  if (job.state === "queued" || job.state === "running") {
    return <InProgressState job={job} pending={pending} onCancel={onCancel} />;
  }

  if (job.state === "failed" || job.state === "cancelled") {
    return (
      <div className="border border-line bg-surface-muted p-3">
        <p className="text-[13px] font-semibold text-ink">
          {job.state === "failed" ? "本次估价未完成" : "估价任务已取消"}
        </p>
        <p className="mt-1 whitespace-pre-line text-[12px] leading-5 text-ink-muted">
          {job.error_message || "没有生成可展示的估价结果。"}
        </p>
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button
            type="button"
            className="btn h-8 text-[12px]"
            disabled={
              pending ||
              !allowRetry ||
              job.error_code === "provider_auth" ||
              job.error_code === "provider_config"
            }
            onClick={() => void onStart(false)}
          >
            重新加入队列
          </button>
          {allowRetry &&
            job.error_code !== "provider_auth" &&
            job.error_code !== "provider_config" && (
              <button
                type="button"
                className="btn h-8 text-[12px]"
                disabled={pending}
                onClick={() => void onStart(true)}
              >
                忽略缓存重试
              </button>
            )}
        </div>
      </div>
    );
  }

  if (job.state === "succeeded" && job.result) {
    return (
      <ValuationResultView
        valuation={job.result}
        compact={compact}
        showDetails={showDetails}
        onToggleDetails={onToggleDetails}
        onRefresh={() => void onStart(true)}
        pending={pending}
        cached={Boolean(job.cached)}
      />
    );
  }

  return (
    <EmptyValuationState
      disabled={pending || !allowRetry}
      reviewRequired={!allowRetry}
      onStart={() => void onStart(false)}
    />
  );
}

function InProgressState({
  job,
  pending,
  onCancel,
}: {
  job: ValuationJob;
  pending: boolean;
  onCancel: () => Promise<void>;
}) {
  return (
    <div className="border border-accent/25 bg-accent-soft/55 p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-[13px] font-semibold text-ink">
            {job.state === "running"
              ? "正在生成域名鉴定报告…"
              : "已加入估价队列"}
          </span>
        </div>
        <span className="mono text-[11px] text-accent">
          {job.cached ? "CACHE HIT" : job.state.toUpperCase()}
        </span>
      </div>
      <div className="mt-3 border-t border-accent/20 pt-2 text-[12px] text-ink-muted">
        <span>入队：{formatRelative(job.queued_at)}</span>
        <span className="ml-3">每日估价不限额</span>
      </div>
      {job.retry_after && (
        <p className="mt-2 text-[12px] text-ink-muted">
          预计重试：{formatDateTime(job.retry_after)}
        </p>
      )}
      {job.state !== "running" && (
        <button
          type="button"
          className="btn mt-3 h-8 text-[12px]"
          disabled={pending}
          onClick={() => void onCancel()}
        >
          取消任务
        </button>
      )}
    </div>
  );
}

function DeferredState({
  job,
  pending,
  allowRetry,
  onRetry,
  onCancel,
}: {
  job: ValuationJob;
  pending: boolean;
  allowRetry: boolean;
  onRetry: () => Promise<void>;
  onCancel: () => Promise<void>;
}) {
  return (
    <div className="rounded-card border border-line bg-surface-muted px-5 py-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-[13px] font-semibold text-ink">
            上游 AI 暂时限流，任务会自动重试
          </p>
          <p className="mt-1 whitespace-pre-line text-[12px] leading-5 text-ink-muted">
            {job.error_message ||
              "AI 提供商暂时不可用，DomainHunter 已保留任务。"}
          </p>
        </div>
        <span className="mono text-[10px] text-ink-muted">DEFERRED</span>
      </div>
      <div className="mt-3 grid gap-1 border-t border-line pt-3 text-[11px] text-ink-muted sm:grid-cols-2">
        <span>入队：{formatRelative(job.queued_at)}</span>
        <span className="sm:text-right">
          {job.retry_after
            ? `自动重试：${formatDateTime(job.retry_after)}`
            : "等待提供商恢复"}
        </span>
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <button
          type="button"
          className="btn h-8 text-[12px]"
          disabled={pending || !allowRetry}
          onClick={() => void onRetry()}
        >
          立即重试
        </button>
        <button
          type="button"
          className="btn h-8 text-[12px]"
          disabled={pending}
          onClick={() => void onCancel()}
        >
          取消任务
        </button>
      </div>
    </div>
  );
}

function ValuationResultView({
  valuation,
  compact,
  showDetails,
  onToggleDetails,
  onRefresh,
  pending,
  cached,
}: {
  valuation: DomainValuation;
  compact: boolean;
  showDetails: boolean;
  onToggleDetails: () => void;
  onRefresh: () => void;
  pending: boolean;
  cached: boolean;
}) {
  const score = valuation.score ?? valuation.quality_score;
  return (
    <div>
      <div
        className="ai-report"
        aria-label={`${valuation.domain} 域名鉴定报告`}
      >
        <ReportLine label="域名" value={valuation.domain} mono />
        <ReportLine label="评分" value={`${score} 分`} emphasis />
        <ReportLine
          label="价格评估"
          value={formatCNY(valuation.price_evaluation_cny)}
          emphasis
        />
        <ReportLine
          label="核心分析"
          value={valuation.core_analysis || valuation.summary}
          multiline
        />
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2 border-y border-line py-2 sm:grid-cols-4">
        <ScoreCell label="评分" score={score} />
        <ScoreCell label="流动性" score={valuation.liquidity_score} />
        <MetricPill
          label="风险"
          value={VALUATION_RISK_LABELS[valuation.risk_level]}
        />
        <MetricPill
          label="可信度"
          value={VALUATION_CONFIDENCE_LABELS[valuation.confidence]}
        />
      </div>
      <div className="mt-3 flex flex-wrap gap-1.5">
        <Pill>风险 {VALUATION_RISK_LABELS[valuation.risk_level]}</Pill>
        <Pill>
          输入充分度 {VALUATION_CONFIDENCE_LABELS[valuation.confidence]}
        </Pill>
        <Pill>
          {valuation.profile_name} · {valuation.model}
        </Pill>
        {cached && (
          <Pill title="命中相同输入与 Prompt 版本的有效结果">缓存结果</Pill>
        )}
      </div>
      {!compact || showDetails ? <ResultDetails valuation={valuation} /> : null}
      <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-line pt-3">
        <button
          type="button"
          className="text-[12px] font-medium text-accent hover:underline"
          onClick={onToggleDetails}
        >
          {showDetails ? "收起研究依据" : "展开研究依据"}
        </button>
        <button
          type="button"
          className="btn h-8 text-[12px]"
          disabled={pending}
          onClick={onRefresh}
        >
          忽略缓存重新估价
        </button>
      </div>
    </div>
  );
}

function ReportLine({
  label,
  value,
  mono = false,
  emphasis = false,
  multiline = false,
}: {
  label: string;
  value: string;
  mono?: boolean;
  emphasis?: boolean;
  multiline?: boolean;
}) {
  return (
    <div
      className={cx("ai-report-line", multiline && "ai-report-line-analysis")}
    >
      <span className="ai-report-label">{label}</span>
      <span
        className={cx(
          "ai-report-value",
          mono && "mono",
          emphasis && "ai-report-emphasis",
          multiline && "ai-report-multiline",
        )}
      >
        {value}
      </span>
    </div>
  );
}

function MetricPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded border border-line bg-surface-muted px-2 py-1 text-[11px]">
      <span className="text-ink-faint">{label}</span>
      <strong className="ml-1 text-ink">{value}</strong>
    </div>
  );
}

function ScoreCell({ label, score }: { label: string; score: number }) {
  return (
    <div className="px-2">
      <div className="mono text-[11px] text-ink-muted">
        {label.toUpperCase()}
      </div>
      <div className="mt-1 text-[24px] font-semibold text-ink">
        {score}
        <span className="ml-0.5 text-[11px] font-normal text-ink-muted">
          /100
        </span>
      </div>
    </div>
  );
}

function ResultDetails({ valuation }: { valuation: DomainValuation }) {
  return (
    <div className="mt-4 grid gap-3 border-t border-line pt-3 sm:grid-cols-3">
      <InsightList
        title="优势"
        values={valuation.strengths}
        tone="text-accent"
        empty="模型未给出明确优势"
      />
      <InsightList
        title="风险"
        values={valuation.risks}
        tone="text-ink-muted"
        empty="模型未报告额外风险"
      />
      <InsightList
        title="数据缺口"
        values={valuation.data_gaps}
        tone="text-ink-muted"
        empty="未报告额外数据缺口"
      />
      <div className="sm:col-span-3 border-t border-line pt-3">
        <p className="mono text-[11px] text-ink-muted">STATUS GUARD</p>
        <p className="mt-1 text-[12px] leading-5 text-ink-muted">
          {valuation.status_guard}
        </p>
        <p className="mt-2 text-[11px] leading-5 text-ink-faint">
          {valuation.disclaimer}
        </p>
      </div>
    </div>
  );
}

function InsightList({
  title,
  values,
  tone,
  empty,
}: {
  title: string;
  values: string[];
  tone: string;
  empty: string;
}) {
  return (
    <div>
      <p className={cx("text-[12px] font-semibold", tone)}>{title}</p>
      <ul className="mt-1.5 space-y-1 text-[12px] leading-5 text-ink-muted">
        {values.length > 0 ? (
          values.slice(0, 3).map((value) => <li key={value}>— {value}</li>)
        ) : (
          <li>— {empty}</li>
        )}
      </ul>
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="animate-pulse space-y-3">
      <div className="h-4 w-32 bg-surface-muted" />
      <div className="h-7 w-56 bg-surface-muted" />
      <div className="h-3 w-full bg-surface-muted" />
      <div className="h-3 w-4/5 bg-surface-muted" />
    </div>
  );
}
