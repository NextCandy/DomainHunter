import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useTheme } from "../lib/theme";
import type { ThemeMode } from "../lib/theme";
import { Logo } from "./Logo";
import { cx, useDensity } from "./ui";

const NAV = [
  { to: "/", label: "概览", detail: "工作台", icon: "home", end: true },
  { to: "/domains", label: "域名资产", detail: "全量清单", icon: "search" },
  { to: "/watchlist", label: "抢注看板", detail: "掉落窗口", icon: "eye" },
  { to: "/history", label: "查询历史", detail: "证据时间线", icon: "history" },
  { to: "/providers", label: "查询源", detail: "服务健康", icon: "providers" },
  { to: "/notifications", label: "通知", detail: "消息中心", icon: "notifications" },
  { to: "/automation", label: "AI 与自动化", detail: "研究任务", icon: "spark" },
  { to: "/settings", label: "系统设置", detail: "运行参数", icon: "settings" },
] as const;

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
  { to: "/notifications", label: "通知", icon: "notifications" },
  { to: "/more", label: "更多", icon: "more" },
] as const;

const MOBILE_MORE = NAV.filter((item) => ["/history", "/providers", "/automation", "/settings"].includes(item.to));

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
  const [mobileMoreOpen, setMobileMoreOpen] = useState(false);

  return (
    <div className="min-h-full min-w-0 bg-canvas">
      <div className="flex min-h-full min-w-0 flex-col lg:flex-row">
        <aside className="hidden lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-[248px] lg:shrink-0 lg:flex-col lg:border-r lg:border-line lg:bg-pure-white lg:px-5 lg:py-7">
          <BrandBlock />
          <div className="mt-12">
            <p className="section-label mb-3 px-3">WORKSPACE</p>
            <nav className="space-y-1" aria-label="主导航">
              {NAV.map((item) => (
                <NavItem key={item.to} item={item} />
              ))}
            </nav>
          </div>

          <div className="mt-auto space-y-4 pt-8">
            <div className="rounded-card border border-line bg-stone-canvas p-4">
              <p className="section-label">SESSION</p>
              <p className="mt-2 truncate text-[13px] font-medium text-ink">{username}</p>
              <p className="mt-0.5 text-[11px] text-ink-muted">DomainHunter {version}</p>
            </div>
            <div className="grid grid-cols-2 gap-2">
              <label className="min-w-0">
                <span className="sr-only">主题</span>
                <select
                  className="input h-9 px-2 text-[11px]"
                  value={mode}
                  onChange={(event) => setMode(event.target.value as ThemeMode)}
                  aria-label="主题"
                >
                  {THEME_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>{option.label}</option>
                  ))}
                </select>
              </label>
              <label className="min-w-0">
                <span className="sr-only">界面密度</span>
                <select
                  className="input h-9 px-2 text-[11px]"
                  value={density}
                  onChange={(event) => setDensity(event.target.value as typeof density)}
                  aria-label="界面密度"
                >
                  {DENSITY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
            </div>
            <button type="button" className="btn btn-ghost w-full" onClick={onLogout}>退出登录</button>
          </div>
        </aside>

        <div className="min-w-0 flex-1">
          <header className="sticky top-0 z-30 border-b border-line bg-pure-white lg:hidden">
            <div className="flex min-h-16 items-center gap-3 px-4">
              <BrandBlock compact />
              <span className="font-mono text-[10px] uppercase tracking-[0.1em] text-ink-faint">WORKSPACE</span>
              <button
                type="button"
                className="btn btn-ghost ml-auto h-9 w-9 px-0"
                onClick={() => setMenuOpen((value) => !value)}
                aria-label="菜单"
                title="打开导航菜单"
                aria-expanded={menuOpen}
              >
                <MenuIcon />
              </button>
            </div>
            {menuOpen && (
              <div className="border-t border-line bg-pure-white px-4 py-3">
                <nav className="grid grid-cols-2 gap-1" aria-label="移动端完整导航">
                  {NAV.map((item) => <NavItem key={item.to} item={item} onClick={() => setMenuOpen(false)} />)}
                </nav>
                <div className="mt-3 grid grid-cols-2 gap-2 border-t border-line pt-3">
                  <label>
                    <span className="label">主题</span>
                    <select className="input h-9 text-[12px]" value={mode} onChange={(event) => setMode(event.target.value as ThemeMode)} aria-label="主题">
                      {THEME_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                    </select>
                  </label>
                  <label>
                    <span className="label">密度</span>
                    <select className="input h-9 text-[12px]" value={density} onChange={(event) => setDensity(event.target.value as typeof density)} aria-label="密度">
                      {DENSITY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                    </select>
                  </label>
                </div>
                <button type="button" className="btn btn-ghost mt-3 w-full" onClick={() => { setMenuOpen(false); onLogout(); }}>退出登录</button>
              </div>
            )}
          </header>

          <main className="page-shell min-w-0 px-4 pb-24 pt-7 sm:px-6 sm:pt-9 lg:px-8 lg:pb-12 lg:pt-12">
            <Outlet />
          </main>

          <nav className="fixed bottom-0 left-0 right-0 z-30 border-t border-line bg-pure-white px-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] pt-1 lg:hidden" aria-label="移动端主导航">
            <div className="mx-auto grid max-w-lg grid-cols-5">
              {MOBILE_NAV.map((item) => item.to === "/more" ? (
                <button
                  key={item.to}
                  type="button"
                  onClick={() => setMobileMoreOpen(true)}
                  className="flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-nav px-1 py-1 text-[10px] text-ink-muted transition-colors hover:text-ink"
                  aria-label="更多页面"
                >
                  <NavIcon name="more" /><span>更多</span>
                </button>
              ) : (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={"end" in item ? item.end : undefined}
                  onClick={() => setMenuOpen(false)}
                  className={({ isActive }) => cx("flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-nav px-1 py-1 text-[10px] transition-colors", isActive ? "bg-sky-wash/60 font-medium text-ink" : "text-ink-muted hover:text-ink")}
                  aria-label={item.label}
                  title={item.label}
                >
                  <NavIcon name={item.icon} />
                  <span>{item.label}</span>
                </NavLink>
              ))}
            </div>
          </nav>

          {mobileMoreOpen && (
            <div className="fixed inset-0 z-50 lg:hidden">
              <button type="button" className="absolute inset-0 bg-overlay/40" onClick={() => setMobileMoreOpen(false)} aria-label="关闭更多页面" />
              <section className="absolute inset-x-0 bottom-0 rounded-t-card border border-line bg-surface-raised p-4 pb-[max(1rem,env(safe-area-inset-bottom))]" role="dialog" aria-modal="true" aria-label="更多页面">
                <div className="mx-auto mb-4 h-1 w-10 rounded-full bg-neutral/30" />
                <h2 className="text-[18px] font-medium">更多</h2>
                <nav className="mt-3 grid grid-cols-2 gap-2">
                  {MOBILE_MORE.map((item) => <NavItem key={item.to} item={item} onClick={() => setMobileMoreOpen(false)} />)}
                </nav>
              </section>
            </div>
          )}

          <footer className="border-t border-line px-4 py-5 text-center font-mono text-[10px] uppercase tracking-[0.08em] text-ink-faint sm:px-6 lg:px-8">
            DomainHunter · {version}
          </footer>
        </div>
      </div>
    </div>
  );
}

function NavItem({ item, onClick }: { item: (typeof NAV)[number]; onClick?: () => void }) {
  return (
    <NavLink
      to={item.to}
      end={"end" in item ? item.end : undefined}
      onClick={onClick}
      className={({ isActive }) => cx("sidebar-link", isActive && "sidebar-link-active")}
      title={item.detail}
    >
      <NavIcon name={item.icon} />
      <span className="min-w-0 flex-1 truncate">{item.label}</span>
      <span className="hidden text-[10px] text-ink-faint xl:block">{item.detail}</span>
    </NavLink>
  );
}

function BrandBlock({ compact = false }: { compact?: boolean }) {
  return (
    <div className={cx("flex min-w-0 items-center gap-2.5", compact && "gap-2")}>
      <span className={cx("flex shrink-0 items-center justify-center rounded-full border border-cyan-edge", compact ? "h-8 w-8" : "h-10 w-10")}>
        <Logo className={cx(compact ? "h-5 w-5" : "h-7 w-7")} />
      </span>
      <div className="min-w-0">
        <div className="brand-wordmark truncate">DomainHunter</div>
        {!compact && <p className="mt-0.5 font-mono text-[9px] uppercase tracking-[0.1em] text-ink-faint">domain intelligence</p>}
      </div>
    </div>
  );
}

function MenuIcon() {
  return <svg aria-hidden="true" className="h-4 w-4" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"><path d="M2.5 4h11M2.5 8h11M2.5 12h11" /></svg>;
}

type NavIconName = "home" | "search" | "eye" | "spark" | "settings" | "history" | "providers" | "notifications" | "more";

function NavIcon({ name }: { name: NavIconName }) {
  const common = { className: "h-4 w-4 shrink-0", viewBox: "0 0 16 16", fill: "none", stroke: "currentColor", strokeWidth: 1.35, strokeLinecap: "round" as const, strokeLinejoin: "round" as const, "aria-hidden": true };
  if (name === "home") return <svg {...common}><path d="m2.5 7.25 5.5-4.5 5.5 4.5v5.25a1 1 0 0 1-1 1h-9a1 1 0 0 1-1-1Z" /><path d="M6.25 13.5v-3h3.5v3" /></svg>;
  if (name === "search") return <svg {...common}><circle cx="7" cy="7" r="3.75" /><path d="m10 10 3.25 3.25" /></svg>;
  if (name === "eye") return <svg {...common}><path d="M1.75 8s2.1-3.25 6.25-3.25S14.25 8 14.25 8s-2.1 3.25-6.25 3.25S1.75 8 1.75 8Z" /><circle cx="8" cy="8" r="1.35" /></svg>;
  if (name === "spark") return <svg {...common}><path d="m8 1.75.95 4.3 4.3.95-4.3.95L8 12.25l-.95-4.3-4.3-.95 4.3-.95Z" /><path d="m12.25 10.75.45 2.05 2.05.45-2.05.45-.45 2.05-.45-2.05-2.05-.45 2.05-.45Z" /></svg>;
  if (name === "history") return <svg {...common}><circle cx="8" cy="8" r="5.5" /><path d="M8 4.75v3.5l2.25 1.25M2.75 3.75v2.5h2.5" /></svg>;
  if (name === "providers") return <svg {...common}><circle cx="3.25" cy="8" r="1.3" /><circle cx="12.75" cy="4" r="1.3" /><circle cx="12.75" cy="12" r="1.3" /><path d="m4.5 7.35 6.9-2.7M4.5 8.65l6.9 2.7" /></svg>;
  if (name === "notifications") return <svg {...common}><path d="M3.25 11.75h9.5l-1.1-1.5V7a3.65 3.65 0 0 0-7.3 0v3.25Z" /><path d="M6.5 13.25a1.75 1.75 0 0 0 3 0" /></svg>;
  if (name === "more") return <svg {...common}><circle cx="3" cy="8" r=".75" fill="currentColor" stroke="none" /><circle cx="8" cy="8" r=".75" fill="currentColor" stroke="none" /><circle cx="13" cy="8" r=".75" fill="currentColor" stroke="none" /></svg>;
  return <svg {...common}><path d="M8 2.25a2 2 0 0 1 2 2v.5h.75a1.5 1.5 0 0 1 1.5 1.5v5.75a1.75 1.75 0 0 1-1.75 1.75h-5A1.75 1.75 0 0 1 3.75 12V6.25a1.5 1.5 0 0 1 1.5-1.5H6v-.5a2 2 0 0 1 2-2Z" /><path d="M6 4.75h4M6.25 8h3.5M6.25 10.5h3.5" /></svg>;
}
