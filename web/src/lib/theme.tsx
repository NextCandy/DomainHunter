import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";

export type ThemeMode = "light" | "dark" | "system";

const STORAGE_KEY = "dh-theme";

interface ThemeContextValue {
  mode: ThemeMode;
  setMode: (mode: ThemeMode) => void;
  resolved: "light" | "dark";
}

const ThemeContext = createContext<ThemeContextValue>({
  mode: "system",
  setMode: () => {},
  resolved: "light",
});

export function useTheme() {
  return useContext(ThemeContext);
}

function readStored(): ThemeMode {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "light" || value === "dark" || value === "system") return value;
  } catch {
    /* localStorage 不可用时退回跟随系统 */
  }
  return "system";
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(readStored);
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches);
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);

  const resolved: "light" | "dark" = mode === "system" ? (systemDark ? "dark" : "light") : mode;

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolved === "dark");
    document.documentElement.style.colorScheme = resolved;
  }, [resolved]);

  const setMode = useCallback((next: ThemeMode) => {
    setModeState(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* 忽略存储失败 */
    }
  }, []);

  const value = useMemo(() => ({ mode, setMode, resolved }), [mode, setMode, resolved]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
