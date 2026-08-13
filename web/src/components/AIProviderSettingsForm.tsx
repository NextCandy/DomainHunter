import { useEffect, useState } from "react";
import type {
  AIProviderProfile,
  AISettings,
  AISettingsInput,
} from "../lib/api";
import { api } from "../lib/api";
import { Spinner, useToast } from "./ui";

const DEFAULT_PROVIDER = "OpenAI Compatible";

export function AIProviderSettingsForm({
  settings,
  profiles,
  models,
  onSaved,
}: {
  settings: AISettings | null;
  profiles: AIProviderProfile[];
  models: string[];
  onSaved: () => void;
}) {
  const [selectedProfileID, setSelectedProfileID] = useState(0);
  const [newProfileMode, setNewProfileMode] = useState(false);
  const [profileName, setProfileName] = useState(DEFAULT_PROVIDER);
  const [isDefault, setIsDefault] = useState(true);
  const [form, setForm] = useState<AISettingsInput>(emptySettings());
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState(false);
  const toast = useToast();

  useEffect(() => {
    if (selectedProfileID > 0 || newProfileMode) return;
    const defaultID =
      settings?.profile_id ||
      profiles.find((profile) => profile.is_default)?.profile_id ||
      0;
    if (defaultID > 0) setSelectedProfileID(defaultID);
  }, [newProfileMode, profiles, settings, selectedProfileID]);

  useEffect(() => {
    const profile = profiles.find(
      (item) => item.profile_id === selectedProfileID,
    );
    if (profile) {
      setProfileName(profile.profile_name);
      setIsDefault(profile.is_default);
      setForm(profileToInput(profile));
      return;
    }
    if (settings && selectedProfileID === 0) {
      setProfileName(settings.profile_name || DEFAULT_PROVIDER);
      setIsDefault(settings.is_default);
      setForm(settingsToInput(settings));
    }
  }, [profiles, selectedProfileID, settings]);

  function set<K extends keyof AISettingsInput>(
    key: K,
    value: AISettingsInput[K],
  ) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  function chooseProfile(id: number) {
    if (id === 0) {
      newProfile();
      return;
    }
    setNewProfileMode(id === 0);
    setSelectedProfileID(id);
    const profile = profiles.find((item) => item.profile_id === id);
    if (profile) {
      setProfileName(profile.profile_name);
      setIsDefault(profile.is_default);
      setForm(profileToInput(profile));
    }
  }

  function newProfile() {
    setSelectedProfileID(0);
    setNewProfileMode(true);
    setProfileName(DEFAULT_PROVIDER);
    setIsDefault(profiles.length === 0);
    setForm(emptySettings());
  }

  async function save() {
    setSaving(true);
    try {
      const input = {
        ...form,
        name: profileName.trim() || DEFAULT_PROVIDER,
        is_default: isDefault,
      };
      if (selectedProfileID > 0) {
        await api.p1.ai.saveSettings({
          ...input,
          profile_id: selectedProfileID,
        });
      } else {
        await api.p1.ai.providers.create(input);
      }
      toast("AI 配置已保存", "success");
      setNewProfileMode(false);
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "AI 配置保存失败", "error");
    } finally {
      setSaving(false);
    }
  }

  async function remove() {
    if (selectedProfileID <= 0 || isDefault) return;
    setRemoving(true);
    try {
      await api.p1.ai.providers.remove(selectedProfileID);
      toast("AI 配置已删除", "success");
      newProfile();
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : "AI 配置删除失败", "error");
    } finally {
      setRemoving(false);
    }
  }

  const selectedProfile = profiles.find(
    (profile) => profile.profile_id === selectedProfileID,
  );
  const keyConfigured = newProfileMode
    ? false
    : (selectedProfile?.api_key_set ?? (settings?.api_key_set || false));
  const keySource = newProfileMode
    ? undefined
    : (selectedProfile?.key_source ?? settings?.key_source);

  return (
    <section className="card min-w-0 p-4">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h2 className="text-[14px] font-semibold">AI Provider 配置</h2>
          <p className="mt-1 text-[12px] text-ink-muted">
            可配置多个 AI；默认项用于 AI Worker
            和域名详情中的研究性估价。请求始终由 Go 后端发起。
          </p>
        </div>
        <span className="shrink-0 rounded-full border border-line px-2 py-0.5 text-[11px] text-ink-muted">
          {keyConfigured ? `Key 已配置（${keySource}）` : "未配置 Key"}
        </span>
      </div>

      <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:items-end">
        <label className="min-w-0 flex-1 text-[12px] text-ink-muted">
          配置档案
          <select
            className="input mt-1"
            value={selectedProfileID}
            onChange={(event) => chooseProfile(Number(event.target.value))}
          >
            {profiles.length === 0 && <option value={0}>新配置</option>}
            {profiles.map((profile) => (
              <option key={profile.profile_id} value={profile.profile_id}>
                {profile.profile_name}
                {profile.is_default ? " · 默认" : ""}
              </option>
            ))}
            {profiles.length > 0 && selectedProfileID === 0 && (
              <option value={0}>新配置</option>
            )}
          </select>
        </label>
        <div className="flex gap-2">
          <button
            type="button"
            className="btn h-9 shrink-0"
            onClick={newProfile}
          >
            新增配置
          </button>
          {selectedProfileID > 0 && !isDefault && (
            <button
              type="button"
              className="btn h-9 shrink-0"
              onClick={() => void remove()}
              disabled={removing}
            >
              {removing && <Spinner />}删除
            </button>
          )}
        </div>
      </div>

      <div className="mt-3 grid min-w-0 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label className="text-[12px] text-ink-muted">
          配置名称
          <input
            className="input mt-1"
            value={profileName}
            onChange={(event) => setProfileName(event.target.value)}
            placeholder={DEFAULT_PROVIDER}
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          提供商
          <input
            className="input mt-1"
            value={providerLabel(form.provider)}
            readOnly
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          API 地址
          <input
            className="input mt-1"
            value={form.base_url}
            onChange={(event) => set("base_url", event.target.value)}
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          模型
          <select
            className="input mt-1"
            value={form.model}
            onChange={(event) => set("model", event.target.value)}
          >
            {Array.from(new Set([form.model, ...models])).map((model) => (
              <option key={model} value={model}>
                {model}
              </option>
            ))}
          </select>
        </label>
        <label className="text-[12px] text-ink-muted">
          API Key（只填新 Key）
          <input
            className="input mt-1"
            type="password"
            autoComplete="new-password"
            value={form.api_key ?? ""}
            onChange={(event) => set("api_key", event.target.value)}
            placeholder={
              keyConfigured ? "已配置，留空不修改" : "不会回显或写入明文"
            }
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          超时（秒）
          <input
            className="input mt-1"
            type="number"
            min="5"
            max="300"
            value={form.timeout_seconds}
            onChange={(event) =>
              set("timeout_seconds", Number(event.target.value))
            }
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          并发（1–4）
          <input
            className="input mt-1"
            type="number"
            min="1"
            max="4"
            value={form.concurrency}
            onChange={(event) => set("concurrency", Number(event.target.value))}
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          最大输出 token
          <input
            className="input mt-1"
            type="number"
            min="128"
            max="8192"
            value={form.max_output_tokens}
            onChange={(event) =>
              set("max_output_tokens", Number(event.target.value))
            }
          />
        </label>
        <label className="text-[12px] text-ink-muted">
          缓存 TTL（秒）
          <input
            className="input mt-1"
            type="number"
            min="300"
            max="2592000"
            value={form.cache_ttl_seconds}
            onChange={(event) =>
              set("cache_ttl_seconds", Number(event.target.value))
            }
          />
        </label>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-[12px] text-ink-muted">
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={isDefault}
            onChange={(event) => setIsDefault(event.target.checked)}
          />
          设为默认配置
        </label>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => set("enabled", event.target.checked)}
          />
          启用 AI Worker
        </label>
      </div>
      <p className="mt-2 text-[11px] text-ink-faint">
        API Key 不会回显；服务端可使用受限环境变量
        DOMAINHUNTER_AI_API_KEY，或配置 DOMAINHUNTER_SECRET_KEY 后加密保存多份
        Key。
      </p>
      <div className="mt-4 flex justify-end">
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => void save()}
          disabled={saving}
        >
          {saving && <Spinner />}保存 AI 配置
        </button>
      </div>
    </section>
  );
}

function providerLabel(provider: string) {
  if (
    provider === "openai_compatible" ||
    provider === "openai-compatible" ||
    provider === "openai"
  )
    return DEFAULT_PROVIDER;
  if (provider === "deepseek") return "DeepSeek";
  return provider || DEFAULT_PROVIDER;
}

function profileToInput(profile: AIProviderProfile): AISettingsInput {
  return {
    profile_id: profile.profile_id,
    name: profile.profile_name,
    is_default: profile.is_default,
    provider: profile.provider,
    base_url: profile.base_url,
    model: profile.model,
    timeout_seconds: profile.timeout_seconds,
    concurrency: profile.concurrency,
    max_output_tokens: profile.max_output_tokens,
    cache_ttl_seconds: profile.cache_ttl_seconds,
    enabled: profile.enabled,
  };
}

function settingsToInput(settings: AISettings): AISettingsInput {
  return {
    profile_id: settings.profile_id,
    name: settings.profile_name,
    is_default: settings.is_default,
    provider: settings.provider,
    base_url: settings.base_url,
    model: settings.model,
    timeout_seconds: settings.timeout_seconds,
    concurrency: settings.concurrency,
    max_output_tokens: settings.max_output_tokens,
    cache_ttl_seconds: settings.cache_ttl_seconds,
    enabled: settings.enabled,
  };
}

function emptySettings(): AISettingsInput {
  return {
    provider: "openai_compatible",
    base_url: "https://api.deepseek.com",
    model: "deepseek-v4-flash",
    timeout_seconds: 30,
    concurrency: 1,
    max_output_tokens: 1200,
    cache_ttl_seconds: 86400,
    enabled: false,
  };
}
