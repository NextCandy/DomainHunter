import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useTheme } from "../lib/theme";
import type { ThemeMode } from "../lib/theme";
import { Logo } from "./Logo";
import { cx, useDensity } from "./ui";

const NAV = [
  { to: "/", label: "概览", end: true },
  { to: "/domains", label: "域名" },
  { to: "/watchlist", label: "抢注看板" },
  { to: "/history", label: "查询历史" },
  { to: "/providers", label: "查询源" },
  { to: "/notifications", label: "通知" },
  { to: "/automation", label: "自动化" },
  { to: "/settings", label: "系统设置" },
];

const THEME_OPTIONS: Array<{ value: ThemeMode; label: string }> = [
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
  { value: "system", label: "跟随系统" },
];

const DENSITY_OPTIONS = [
  { value: "comfortable", label: "宽松" },
  { value: "compact", label: "标准" },
  { value: "dense", label: "紧凑" },
] as const;

const MOBILE_NAV = [
  { to: "/", label: "概览", icon: "home", end: true },
  { to: "/domains", label: "域名", icon: "search" },
  { to: "/watchlist", label: "抢注", icon: "eye" },
  { to: "/history", label: "历史", icon: "history" },
  { to: "/settings", label: "设置", icon: "settings" },
] as const;

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
  const { density, setDensity } = useDensity();
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-30 border-b border-line bg-surface-raised/95 backdrop-blur">
        <div className="mx-auto flex h-12 max-w-[1600px] items-center gap-3 px-3 sm:px-5">
          <div className="flex items-center gap-2">
            <Logo className="h-6 w-6" />
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
            <select
              className="input hidden h-7 w-[84px] py-0 text-[12px] sm:block"
              value={density}
              onChange={(event) => setDensity(event.target.value as typeof density)}
              aria-label="界面密度"
              title="界面密度"
            >
              {DENSITY_OPTIONS.map((option) => (
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
              title="打开导航菜单"
              aria-expanded={menuOpen}
            >
              <MenuIcon />
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
            <label className="col-span-2 flex items-center gap-2 border-t border-line pt-2 text-[12px] text-ink-muted sm:col-span-4">
              <span>界面密度</span>
              <select
                className="input h-7 w-[84px] py-0 text-[12px]"
                value={density}
                onChange={(event) => setDensity(event.target.value as typeof density)}
                aria-label="界面密度"
              >
                {DENSITY_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          </nav>
        )}
      </header>

      <main className="mx-auto w-full max-w-[1600px] flex-1 px-3 pb-20 pt-4 sm:px-5 sm:py-6 lg:pb-6">
        <Outlet />
      </main>

      <nav
        className="fixed bottom-0 left-0 right-0 z-30 border-t border-line bg-surface-raised/95 px-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] pt-1 backdrop-blur lg:hidden"
        aria-label="移动端主导航"
      >
        <div className="mx-auto grid max-w-lg grid-cols-5">
          {MOBILE_NAV.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={"end" in item ? item.end : undefined}
              className={({ isActive }) =>
                cx(
                  "flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-md px-1 py-1 text-[10px] transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-accent",
                  isActive ? "font-medium text-accent" : "text-ink-muted hover:text-ink",
                )
              }
              aria-label={item.label}
              title={item.label}
            >
              <NavIcon name={item.icon} />
              <span>{item.label}</span>
            </NavLink>
          ))}
        </div>
      </nav>

      <footer className="border-t border-line px-3 py-3 pb-20 text-center text-[11px] text-ink-faint sm:px-5 lg:pb-3">
        DomainHunter {version}
      </footer>
    </div>
  );
}

function MenuIcon() {
  return (
    <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round">
      <path d="M2.5 4h11M2.5 8h11M2.5 12h11" />
    </svg>
  );
}

function NavIcon({ name }: { name: (typeof MOBILE_NAV)[number]["icon"] }) {
  const common = {
    className: "h-4 w-4",
    viewBox: "0 0 16 16",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: 1.35,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true,
  };

  if (name === "home") {
    return (
      <svg {...common}>
        <path d="m2.5 7.25 5.5-4.5 5.5 4.5v5.25a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1Z" />
        <path d="M6.25 13.5v-3h3.5v3" />
      </svg>
    );
  }
  if (name === "search") {
    return (
      <svg {...common}>
        <circle cx="7" cy="7" r="3.75" />
        <path d="m10 10 3.25 3.25" />
      </svg>
    );
  }
  if (name === "eye") {
    return (
      <svg {...common}>
        <path d="M1.75 8s2.1-3.25 6.25-3.25S14.25 8 14.25 8s-2.1 3.25-6.25 3.25S1.75 8 1.75 8Z" />
        <circle cx="8" cy="8" r="1.35" />
      </svg>
    );
  }
  if (name === "history") {
    return (
      <svg {...common}>
        <path d="M3 5.25A5.3 5.3 0 1 1 2.75 9" />
        <path d="M2.25 3.5v2.75H5" />
        <path d="M8 5.25V8l1.75 1" />
      </svg>
    );
  }
  return (
    <svg {...common}>
      <path d="M8 2.25a2 2 0 0 1 2 2v.5h.75a1.5 1.5 0 0 1 1.5 1.5v5.75a1.75 1.75 0 0 1-1.75 1.75h-5A1.75 1.75 0 0 1 3.75 12V6.25a1.5 1.5 0 0 1 1.5-1.5H6v-.5a2 2 0 0 1 2-2Z" />
      <path d="M6 4.75h4M6.25 8h3.5M6.25 10.5h3.5" />
    </svg>
  );
}
