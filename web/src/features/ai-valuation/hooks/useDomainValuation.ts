import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, UnauthorizedError } from "../../../lib/api";
import { valuationApi } from "../lib/valuation-api";
import type {
  EnqueueValuationInput,
  ValuationJob,
} from "../lib/valuation-types";
import { isTerminalJobState } from "../lib/valuation-types";

const POLL_INTERVAL_MS = 2_500;

export interface UseDomainValuationOptions {
  enabled?: boolean;
  onUnauthorized?: () => void;
  onCompleted?: (job: ValuationJob) => void;
}

export interface DomainValuationController {
  job: ValuationJob | null;
  loading: boolean;
  submitting: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  enqueue: (input?: EnqueueValuationInput) => Promise<ValuationJob | null>;
  cancel: () => Promise<void>;
  retry: () => Promise<void>;
  clearError: () => void;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 409) {
      if (error.message.includes("重试") || error.message.includes("任务状态"))
        return error.message;
      return "该域名已在估价队列中，请等待当前任务完成。";
    }
    if (error.status === 422) {
      if (
        error.message.includes("API Key") ||
        error.message.includes("模型") ||
        error.message.includes("接口地址")
      )
        return error.message;
      return "当前域名状态或证据不足，暂不能加入 AI 估价。";
    }
    if (error.status === 429) return "AI Provider 暂时限流，请稍后重试。";
    if (error.status === 401) return "登录状态已失效，请重新登录后再试。";
  }
  return error instanceof Error ? error.message : "AI 估价请求失败";
}

export function useDomainValuation(
  domain: string | null | undefined,
  options: UseDomainValuationOptions = {},
): DomainValuationController {
  const { enabled = true, onUnauthorized, onCompleted } = options;
  const [job, setJob] = useState<ValuationJob | null>(null);
  const [loading, setLoading] = useState(Boolean(domain));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const completionRef = useRef<string | null>(null);

  const refresh = useCallback(async () => {
    if (!domain || !enabled) {
      setJob(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const current = await valuationApi.valuations.getCurrent(domain);
      setJob(current);
      setError(null);
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }, [domain, enabled, onUnauthorized]);

  useEffect(() => {
    completionRef.current = null;
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!job || isTerminalJobState(job.state) || !enabled) return;
    const timer = window.setInterval(async () => {
      try {
        const next = await valuationApi.valuations.getJob(job.id);
        setJob(next);
        if (
          isTerminalJobState(next.state) &&
          completionRef.current !== next.id
        ) {
          completionRef.current = next.id;
          onCompleted?.(next);
        }
      } catch (requestError) {
        if (requestError instanceof UnauthorizedError) onUnauthorized?.();
        setError(errorMessage(requestError));
      }
    }, POLL_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [enabled, job, onCompleted, onUnauthorized]);

  const enqueue = useCallback(
    async (input: EnqueueValuationInput = {}) => {
      if (!domain || submitting) return null;
      setSubmitting(true);
      setError(null);
      try {
        const created = await valuationApi.valuations.enqueue(domain, {
          priority: "normal",
          force_refresh: false,
          ...input,
        });
        setJob(created);
        return created;
      } catch (requestError) {
        if (requestError instanceof UnauthorizedError) onUnauthorized?.();
        setError(errorMessage(requestError));
        return null;
      } finally {
        setSubmitting(false);
      }
    },
    [domain, onUnauthorized, submitting],
  );

  const cancel = useCallback(async () => {
    if (!job || isTerminalJobState(job.state)) return;
    setSubmitting(true);
    setError(null);
    try {
      const cancelled = await valuationApi.valuations.cancel(job.id);
      setJob(cancelled);
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(errorMessage(requestError));
    } finally {
      setSubmitting(false);
    }
  }, [job, onUnauthorized]);

  const retry = useCallback(async () => {
    if (!job || job.state !== "deferred") return;
    setSubmitting(true);
    setError(null);
    try {
      const queued = await valuationApi.valuations.retry(job.id);
      setJob(queued);
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(errorMessage(requestError));
    } finally {
      setSubmitting(false);
    }
  }, [job, onUnauthorized]);

  return {
    job,
    loading,
    submitting,
    error,
    refresh,
    enqueue,
    cancel,
    retry,
    clearError: () => setError(null),
  };
}
