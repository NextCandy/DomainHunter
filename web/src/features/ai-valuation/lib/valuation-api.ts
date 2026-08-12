import { api } from "../../../lib/api";
import type {
  AIProfile,
  AIProfileInput,
  ConnectionTestResult,
  EnqueueValuationInput,
  ValuationJob,
  ValuationPolicy,
} from "./valuation-types";

/**
 * 后端契约：所有请求由 DomainHunter 同源后端发起。
 * 浏览器从不直接请求 DeepSeek，也不读取 API Key。
 */
export const valuationApi = {
  getPolicy: () => api.get<ValuationPolicy>("/api/v2/ai/valuation-policy"),

  profiles: {
    list: () => api.get<{ profiles: AIProfile[] }>("/api/v2/ai/profiles"),
    create: (input: AIProfileInput) => api.post<AIProfile>("/api/v2/ai/profiles", input),
    update: (id: string, input: AIProfileInput) => api.put<AIProfile>(`/api/v2/ai/profiles/${encodeURIComponent(id)}`, input),
    remove: (id: string) => api.delete<{ status: "deleted" }>(`/api/v2/ai/profiles/${encodeURIComponent(id)}`),
    testConnection: (input: Pick<AIProfileInput, "provider" | "base_url" | "model" | "api_key" | "thinking_type" | "reasoning_effort" | "timeout_seconds">) =>
      api.post<ConnectionTestResult>("/api/v2/ai/profiles/test-connection", input),
  },

  valuations: {
    getCurrent: (domain: string) =>
      api.getOptional<ValuationJob>(`/api/v2/domains/${encodeURIComponent(domain)}/valuation`),
    enqueue: (domain: string, input: EnqueueValuationInput) =>
      api.post<ValuationJob>(`/api/v2/domains/${encodeURIComponent(domain)}/valuation`, input),
    getJob: (jobId: string) => api.get<ValuationJob>(`/api/v2/ai/jobs/${encodeURIComponent(jobId)}`),
    cancel: (jobId: string) => api.post<ValuationJob>(`/api/v2/ai/jobs/${encodeURIComponent(jobId)}/cancel`),
  },
};
