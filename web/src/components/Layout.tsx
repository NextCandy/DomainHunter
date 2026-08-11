import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useTheme } from "../lib/theme";
import type { ThemeMode } from "../lib/theme";
import { cx } from "./ui";

const NAV = [
  { to: "/", label: "概览", end: true },
  { to: "/domains", label: "域名" },
  { to: "/watchlist", label: "抢注看板" },
  { to: "/history", label: "查询历史" },
  { to: "/providers", label: "查询源" },
  { to: "/notifications", label: "通知" },
  { to: "/settings", label: "系统设置" },
];

const THEME_OPTIONS: Array<{ value: ThemeMode; label: string }> = [
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
  { value: "system", label: "跟随系统" },
];

export function Layout({
  username,
  version,
  onLogout,
}: {
  username: string;
  version: string;
  onLogout: () => void;
}) {
  const { mode, setMode } = useTheme();
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-30 border-b border-line bg-surface-raised/95 backdrop-blur">
        <div className="mx-auto flex h-12 max-w-[1600px] items-center gap-3 px-3 sm:px-5">
          <div className="flex items-center gap-2">
            <span className="grid h-6 w-6 place-items-center rounded bg-ink text-[11px] font-bold text-surface-raised">
              DH
            </span>
            <span className="text-[14px] font-semibold tracking-tight">DomainHunter</span>
          </div>

          <nav className="ml-4 hidden items-center gap-0.5 lg:flex">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cx(
                    "rounded-md px-2.5 py-1.5 text-[13px] transition-colors",
                    isActive
                      ? "bg-surface-muted font-medium text-ink"
                      : "text-ink-muted hover:bg-surface-muted hover:text-ink",
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>

          <div className="ml-auto flex items-center gap-2">
            <select
              className="input h-7 w-[104px] py-0 text-[12px]"
              value={mode}
              onChange={(event) => setMode(event.target.value as ThemeMode)}
              aria-label="主题"
            >
              {THEME_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <span className="hidden text-[12px] text-ink-muted sm:inline">{username}</span>
            <button type="button" className="btn h-7 px-2 text-[12px]" onClick={onLogout}>
              退出
            </button>
            <button
              type="button"
              className="btn btn-ghost h-7 px-2 lg:hidden"
              onClick={() => setMenuOpen((value) => !value)}
              aria-label="菜单"
              aria-expanded={menuOpen}
            >
              ☰
            </button>
          </div>
        </div>

        {menuOpen && (
          <nav className="grid grid-cols-2 gap-1 border-t border-line px-3 py-2 sm:grid-cols-4 lg:hidden">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                onClick={() => setMenuOpen(false)}
                className={({ isActive }) =>
                  cx(
                    "rounded-md px-2.5 py-2 text-[13px]",
                    isActive ? "bg-surface-muted font-medium text-ink" : "text-ink-muted",
                  )
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
        )}
      </header>

      <main className="mx-auto w-full max-w-[1600px] flex-1 px-3 py-4 sm:px-5 sm:py-6">
        <Outlet />
      </main>

      <footer className="border-t border-line px-3 py-3 text-center text-[11px] text-ink-faint sm:px-5">
        DomainHunter {version} · 数据文件 data/puff.db
      </footer>
    </div>
  );
}
