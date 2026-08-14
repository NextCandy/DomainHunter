import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";

export type Locale = "zh-CN" | "en-US";

const STORAGE_KEY = "dh-locale";

const MESSAGES: Record<Locale, Record<string, string>> = {
  "zh-CN": {
    "nav.overview": "概览",
    "nav.domains": "域名资产",
    "nav.watchlist": "抢注看板",
    "nav.history": "查询历史",
    "nav.providers": "查询源",
    "nav.notifications": "通知",
    "nav.automation": "AI 与自动化",
    "nav.settings": "系统设置",
    "nav.workspace": "工作区",
    "common.command": "命令面板",
    "common.search": "搜索命令…",
    "common.close": "关闭",
    "common.theme": "主题",
    "common.density": "密度",
    "common.light": "浅色",
    "common.dark": "深色",
    "common.system": "跟随系统",
    "common.comfortable": "宽松",
    "common.compact": "标准",
    "common.dense": "紧凑",
    "common.language": "语言",
    "breadcrumb.home": "概览",
    "breadcrumb.domains": "域名资产",
    "breadcrumb.watchlist": "抢注看板",
    "breadcrumb.history": "查询历史",
    "breadcrumb.providers": "查询源",
    "breadcrumb.notifications": "通知",
    "breadcrumb.automation": "AI 与自动化",
    "breadcrumb.settings": "系统设置",
  },
  "en-US": {
    "nav.overview": "Overview",
    "nav.domains": "Domains",
    "nav.watchlist": "Watchlist",
    "nav.history": "History",
    "nav.providers": "Providers",
    "nav.notifications": "Notifications",
    "nav.automation": "AI & Automation",
    "nav.settings": "Settings",
    "nav.workspace": "Workspace",
    "common.command": "Command palette",
    "common.search": "Search commands…",
    "common.close": "Close",
    "common.theme": "Theme",
    "common.density": "Density",
    "common.light": "Light",
    "common.dark": "Dark",
    "common.system": "System",
    "common.comfortable": "Comfortable",
    "common.compact": "Compact",
    "common.dense": "Dense",
    "common.language": "Language",
    "breadcrumb.home": "Overview",
    "breadcrumb.domains": "Domains",
    "breadcrumb.watchlist": "Watchlist",
    "breadcrumb.history": "History",
    "breadcrumb.providers": "Providers",
    "breadcrumb.notifications": "Notifications",
    "breadcrumb.automation": "AI & Automation",
    "breadcrumb.settings": "Settings",
  },
};

interface LocaleContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: (key: string, fallback?: string) => string;
}

const LocaleContext = createContext<LocaleContextValue>({
  locale: "zh-CN",
  setLocale: () => {},
  t: (key, fallback) => fallback ?? key,
});

function readLocale(): Locale {
  try {
    return localStorage.getItem(STORAGE_KEY) === "en-US" ? "en-US" : "zh-CN";
  } catch {
    return "zh-CN";
  }
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readLocale);
  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    document.documentElement.lang = next;
    try { localStorage.setItem(STORAGE_KEY, next); } catch { /* storage is optional */ }
  }, []);

  useEffect(() => { document.documentElement.lang = locale; }, [locale]);

  const value = useMemo<LocaleContextValue>(() => ({
    locale,
    setLocale,
    t: (key, fallback) => MESSAGES[locale][key] ?? fallback ?? key,
  }), [locale, setLocale]);

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useLocale() {
  return useContext(LocaleContext);
}
