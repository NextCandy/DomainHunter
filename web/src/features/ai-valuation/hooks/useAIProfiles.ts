import { useCallback, useEffect, useMemo, useState } from "react";
import { UnauthorizedError } from "../../../lib/api";
import { valuationApi } from "../lib/valuation-api";
import type { AIProfile, AIProfileInput, ConnectionTestResult } from "../lib/valuation-types";

export interface UseAIProfilesOptions {
  enabled?: boolean;
  onUnauthorized?: () => void;
}

export interface AIProfilesController {
  profiles: AIProfile[];
  defaultProfile: AIProfile | null;
  loading: boolean;
  saving: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  save: (input: AIProfileInput, id?: string) => Promise<AIProfile | null>;
  remove: (id: string) => Promise<boolean>;
  testConnection: (input: Pick<AIProfileInput, "provider" | "base_url" | "model" | "api_key" | "thinking_type" | "reasoning_effort" | "timeout_seconds">) => Promise<ConnectionTestResult | null>;
  clearError: () => void;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : "AI 配置操作失败";
}

export function useAIProfiles(options: UseAIProfilesOptions = {}): AIProfilesController {
  const { enabled = true, onUnauthorized } = options;
  const [profiles, setProfiles] = useState<AIProfile[]>([]);
  const [loading, setLoading] = useState(enabled);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!enabled) {
      setProfiles([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const response = await valuationApi.profiles.list();
      setProfiles(response.profiles ?? []);
      setError(null);
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(message(requestError));
    } finally {
      setLoading(false);
    }
  }, [enabled, onUnauthorized]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const save = useCallback(async (input: AIProfileInput, id?: string) => {
    setSaving(true);
    setError(null);
    try {
      const profile = id
        ? await valuationApi.profiles.update(id, input)
        : await valuationApi.profiles.create(input);
      await refresh();
      return profile;
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(message(requestError));
      return null;
    } finally {
      setSaving(false);
    }
  }, [onUnauthorized, refresh]);

  const remove = useCallback(async (id: string) => {
    setSaving(true);
    setError(null);
    try {
      await valuationApi.profiles.remove(id);
      await refresh();
      return true;
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(message(requestError));
      return false;
    } finally {
      setSaving(false);
    }
  }, [onUnauthorized, refresh]);

  const testConnection = useCallback(async (input: Pick<AIProfileInput, "provider" | "base_url" | "model" | "api_key" | "thinking_type" | "reasoning_effort" | "timeout_seconds">) => {
    setSaving(true);
    setError(null);
    try {
      return await valuationApi.profiles.testConnection(input);
    } catch (requestError) {
      if (requestError instanceof UnauthorizedError) onUnauthorized?.();
      setError(message(requestError));
      return null;
    } finally {
      setSaving(false);
    }
  }, [onUnauthorized]);

  const defaultProfile = useMemo(
    () => profiles.find((profile) => profile.is_default) ?? profiles.find((profile) => profile.enabled) ?? null,
    [profiles],
  );

  return { profiles, defaultProfile, loading, saving, error, refresh, save, remove, testConnection, clearError: () => setError(null) };
}
