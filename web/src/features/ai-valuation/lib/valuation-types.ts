import type { DomainStatus } from "../../../lib/api";

export type AIProfileStatus = "ready" | "not_configured" | "degraded" | "disabled";
export type AIProvider = "deepseek" | "openai_compatible";
export type ThinkingType = "disabled" | "enabled";
export type ReasoningEffort = "low" | "high" | "max";
export type ValuationJobState = "idle" | "queued" | "running" | "succeeded" | "failed" | "cancelled" | "deferred";

export interface AIProfile {
  id: string;
  name: string;
  provider: AIProvider;
  enabled: boolean;
  is_default: boolean;
  status: AIProfileStatus;
  base_url: string;
  base_url_host: string;
  model: string;
  api_key_set: boolean;
  api_key_source: "none" | "environment" | "encrypted_store";
  thinking_type: ThinkingType;
  reasoning_effort: ReasoningEffort;
  timeout_seconds: number;
  max_tokens: number;
  concurrency: number;
  daily_limit: number;
  cache_ttl_hours: number;
  last_tested_at?: string;
  last_test_latency_ms?: number;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

export interface AIProfileInput {
  name: string;
  provider: AIProvider;
  enabled: boolean;
  is_default: boolean;
  base_url: string;
  model: string;
  api_key?: string;
  thinking_type: ThinkingType;
  reasoning_effort: ReasoningEffort;
  timeout_seconds: number;
  max_tokens: number;
  concurrency: number;
  daily_limit: number;
  cache_ttl_hours: number;
}

export interface DomainValuation {
  id: string;
  domain: string;
  profile_id: string;
  profile_name: string;
  provider: string;
  model: string;
  prompt_version: string;
  input_fingerprint: string;
  score?: number;
  quality_score: number;
  liquidity_score: number;
  risk_level: "low" | "medium" | "high";
  confidence: "low" | "medium" | "high";
  indicative_value_usd?: { low: number; high: number; currency: string };
  price_evaluation_cny?: { low: number; high: number; currency: "CNY" | string };
  summary: string;
  core_analysis?: string;
  strengths: string[];
  risks: string[];
  data_gaps: string[];
  evidence_used: string[];
  status_guard: string;
  disclaimer: string;
  created_at: string;
  expires_at?: string;
}

export interface ValuationQuota {
  used_today: number;
  daily_limit: number;
  remaining_today: number;
}

export interface ValuationJob {
  id: string;
  domain: string;
  state: ValuationJobState;
  profile_id: string;
  priority: "normal" | "high";
  queued_at: string;
  started_at?: string;
  completed_at?: string;
  result?: DomainValuation;
  retry_after?: string;
  error_code?: string;
  error_message?: string;
  cached?: boolean;
  quota?: ValuationQuota;
}

export interface EnqueueValuationInput {
  profile_id?: string;
  priority?: "normal" | "high";
  force_refresh?: boolean;
}

export interface ConnectionTestResult {
  ok: boolean;
  status: AIProfileStatus;
  latency_ms?: number;
  model?: string;
  normalized_base_url?: string;
  message: string;
}

export interface ValuationPolicy {
  status_guard: string;
  research_only_disclaimer: string;
  raw_whois_sent: false;
  user_note_sent: false;
}

export const VALUATION_STATE_LABELS: Record<ValuationJobState, string> = {
  idle: "尚未估价",
  queued: "排队中",
  running: "分析中",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  deferred: "待重试",
};

export const VALUATION_RISK_LABELS: Record<DomainValuation["risk_level"], string> = {
  low: "低",
  medium: "中",
  high: "高",
};

export const VALUATION_CONFIDENCE_LABELS: Record<DomainValuation["confidence"], string> = {
  low: "低",
  medium: "中",
  high: "高",
};

export function isTerminalJobState(state: ValuationJobState): boolean {
  return state === "succeeded" || state === "failed" || state === "cancelled";
}

export function formatUSD(value?: DomainValuation["indicative_value_usd"]): string {
  if (!value) return "研究区间不可用";
  const formatter = new Intl.NumberFormat("en-US", { style: "currency", currency: value.currency || "USD", maximumFractionDigits: 0 });
  return `${formatter.format(value.low)} – ${formatter.format(value.high)}`;
}

export function formatCNY(value?: DomainValuation["price_evaluation_cny"]): string {
  if (!value) return "人民币价格区间待刷新";
  const formatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 });
  return `${formatter.format(value.low)}–${formatter.format(value.high)} 元`;
}

export type { DomainStatus };
