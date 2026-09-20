import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation, useSearchParams } from "react-router-dom";
import { useTheme } from "../lib/theme";
import type { ThemeMode } from "../lib/theme";
import { Logo } from "./Logo";
import { cx, useDensity } from "./ui";
import { Breadcrumb } from "./Breadcrumb";
import { CommandPalette } from "./CommandPalette";
import { AuroraBackground } from "./AuroraBackground";
import { useLocale } from "../lib/i18n";

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

const SIDEBAR_COLLAPSE_KEY = "dh-sidebar-collapsed";

const MOBILE_MORE = NAV.filter((item) => ["/history", "/providers", "/automation", "/settings"].includes(item.to));

const NAV_LABEL_KEYS: Record<string, string> = {
  "/": "nav.overview",
  "/domains": "nav.domains",
  "/watchlist": "nav.watchlist",
  "/history": "nav.history",
  "/providers": "nav.providers",
  "/notifications": "nav.notifications",
  "/automation": "nav.automation",
  "/settings": "nav.settings",
};

const MOBILE_LABEL_KEYS: Record<string, string> = {
  "/": "nav.overview",
  "/domains": "nav.domains",
  "/watchlist": "nav.watchlist",
  "/notifications": "nav.notifications",
  "/more": "nav.workspace",
};

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
  const { locale, setLocale, t } = useLocale();
  const location = useLocation();
  const [, setSearchParams] = useSearchParams();
  const [menuOpen, setMenuOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_COLLAPSE_KEY) === "1";
    } catch {
      return false;
    }
  });
  const [appearanceOpen, setAppearanceOpen] = useState(false);

  const toggleSidebar = useCallback((collapsed: boolean) => {
    setSidebarCollapsed(collapsed);
    try {
      localStorage.setItem(SIDEBAR_COLLAPSE_KEY, collapsed ? "1" : "0");
    } catch {
      /* 存不下就只在本次会话生效 */
    }
  }, []);
  const [mobileMoreOpen, setMobileMoreOpen] = useState(false);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);

  function changeDensity(value: string) {
    const next = value as typeof density;
    setDensity(next);
    if (location.pathname === "/domains") {
      const params = new URLSearchParams(location.search);
      params.set("density", next);
      setSearchParams(params, { replace: true });
    }
  }

  useEffect(() => {
    const open = () => setShortcutsOpen(true);
    window.addEventListener("domainhunter:shortcuts", open);
    return () => window.removeEventListener("domainhunter:shortcuts", open);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen(true);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  return (
    // 氛围层在最底（-z-10），内容不设不透明底色，让底光从半透明面板后面透上来。
    <div className="relative min-h-full min-w-0">
      <AuroraBackground />
      <div className="flex min-h-full min-w-0 flex-col lg:flex-row">
        {/* 侧栏：198px、sticky 全高，80% 不透明 + 背板模糊。深度只来自发丝线。 */}
        <aside
          className={cx(
            "hidden lg:sticky lg:top-0 lg:flex lg:h-screen lg:shrink-0 lg:flex-col lg:border-r lg:border-line lg:bg-sidebar/80 lg:py-5 lg:backdrop-blur-chrome",
            sidebarCollapsed ? "lg:w-[68px] lg:px-2" : "lg:w-nav lg:px-3.5",
          )}
        >
          {sidebarCollapsed ? (
            <div className="flex justify-center" title={`DomainHunter ${version} · domain intelligence`}>
              <Logo className="h-6 w-6 shrink-0 text-ink" />
            </div>
          ) : (
            <BrandBlock />
          )}
          <div className="mt-8">
            {!sidebarCollapsed && <p className="section-label mb-2 px-2.5">WORKSPACE</p>}
            <nav className="enter-stagger space-y-0.5" aria-label="主导航">
              {NAV.map((item) => (
                <NavItem key={item.to} item={item} iconOnly={sidebarCollapsed} />
              ))}
            </nav>
          </div>

          <div className="mt-auto space-y-2 pt-6">
            {sidebarCollapsed ? (
              <button
                type="button"
                className="btn btn-ghost h-9 w-full px-0"
                onClick={onLogout}
                aria-label="退出登录"
                title={`${username} · DomainHunter ${version} · 退出登录`}
              >
                ⏻
              </button>
            ) : (
              <>
                <div className="rounded-card border border-line bg-raised/50 p-3">
                  <p className="section-label">SESSION</p>
                  <p className="mt-1.5 truncate text-[13px] text-ink">{username}</p>
                  <p className="mt-0.5 font-mono text-[10.5px] text-ink-faint">DomainHunter {version}</p>
                </div>
                <button type="button" className="btn btn-ghost w-full" onClick={onLogout}>退出登录</button>
              </>
            )}
            {/* 不用侧栏名称的人可以把它收成图标条，把 130px 还给表格。 */}
            <button
              type="button"
              className="w-full rounded-md py-1 text-[11px] text-ink-faint hover:text-accent"
              onClick={() => toggleSidebar(!sidebarCollapsed)}
              aria-label={sidebarCollapsed ? "展开侧栏" : "折叠侧栏"}
              title={sidebarCollapsed ? "展开侧栏" : "折叠侧栏"}
            >
              {sidebarCollapsed ? "»" : "« 折叠侧栏"}
            </button>
          </div>
        </aside>

        <div className="min-w-0 flex-1">
          <header className="sticky top-0 z-30 hidden min-h-14 items-center justify-end gap-3 border-b border-line bg-canvas/80 px-6 backdrop-blur-chrome lg:flex">
            {/* 主题 / 密度 / 语言原来是三组常驻 toggle，横着占掉小半条顶栏，
                而它们一天也调不了一次。收进一个「外观」popover。 */}
            <div className="relative">
              <button
                type="button"
                className="btn btn-ghost"
                onClick={() => setAppearanceOpen((value) => !value)}
                aria-expanded={appearanceOpen}
                aria-haspopup="dialog"
              >
                外观 <span aria-hidden="true">⌄</span>
              </button>
              {appearanceOpen && (
                <>
                  <div className="fixed inset-0 z-30" onClick={() => setAppearanceOpen(false)} role="presentation" />
                  <div className="absolute right-0 top-11 z-40 w-[248px] space-y-3 rounded-card border border-line bg-surface-raised p-3 shadow-lg">
                    <SegmentedControl label="主题" value={mode} options={THEME_OPTIONS} onChange={(value) => setMode(value as ThemeMode)} />
                    <SegmentedControl label="密度" value={density} options={DENSITY_OPTIONS} onChange={changeDensity} />
                    <SegmentedControl label={t("common.language")} value={locale} options={[{ value: "zh-CN", label: "中文" }, { value: "en-US", label: "EN" }]} onChange={(value) => setLocale(value as "zh-CN" | "en-US")} />
                  </div>
                </>
              )}
            </div>
            <button type="button" className="btn btn-ghost" onClick={() => setCommandOpen(true)} aria-label={t("common.command")} title="Command palette (⌘/Ctrl K)"><kbd className="font-mono text-[10.5px]">⌘K</kbd></button>
          </header>
          <header className="sticky top-0 z-30 border-b border-line bg-canvas/80 backdrop-blur-chrome lg:hidden">
            <div className="flex min-h-14 items-center gap-3 px-4">
              <BrandBlock compact />
              <span className="section-label">WORKSPACE</span>
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
              <div className="border-t border-line bg-surface px-4 py-3">
                {/* 底栏已经占着概览 / 域名 / 抢注 / 通知四个主入口，汉堡里再列一遍
                    全部八个页面就是两套导航抢同一块空间。这里只给底栏「更多」里的
                    次要页，加上底栏放不下的主题 / 密度 / 语言 / 退出。 */}
                <nav className="grid grid-cols-2 gap-1" aria-label="次要页面导航">
                  {MOBILE_MORE.map((item) => <NavItem key={item.to} item={item} onClick={() => setMenuOpen(false)} />)}
                </nav>
                <div className="mt-3 space-y-3 border-t border-line pt-3">
                  <SegmentedControl label={t("common.theme")} value={mode} options={THEME_OPTIONS} onChange={(value) => setMode(value as ThemeMode)} />
                  <SegmentedControl label={t("common.density")} value={density} options={DENSITY_OPTIONS} onChange={changeDensity} />
                  <SegmentedControl label={t("common.language")} value={locale} options={[{ value: "zh-CN", label: "中文" }, { value: "en-US", label: "EN" }]} onChange={(value) => setLocale(value as "zh-CN" | "en-US")} />
                </div>
                <button type="button" className="btn btn-ghost mt-3 w-full" onClick={() => { setMenuOpen(false); onLogout(); }}>退出登录</button>
              </div>
            )}
          </header>

          <main className="page-shell min-w-0 px-4 pb-24 pt-7 sm:px-6 sm:pt-9 lg:px-8 lg:pb-12 lg:pt-12">
            <Breadcrumb />
            <Outlet />
          </main>

          <nav className="fixed bottom-0 left-0 right-0 z-30 border-t border-line bg-canvas/80 px-1 pb-[max(0.25rem,env(safe-area-inset-bottom))] pt-1 backdrop-blur-chrome lg:hidden" aria-label="移动端主导航">
            <div className="mx-auto grid max-w-lg grid-cols-5">
              {MOBILE_NAV.map((item) => item.to === "/more" ? (
                <button
                  key={item.to}
                  type="button"
                  onClick={() => setMobileMoreOpen(true)}
                  className="flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-nav px-1 py-1 text-[10px] text-ink-muted transition-colors hover:text-ink"
                  aria-label="更多页面"
                >
                  <NavIcon name="more" /><span>{t(MOBILE_LABEL_KEYS[item.to], item.label)}</span>
                </button>
              ) : (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={"end" in item ? item.end : undefined}
                  onClick={() => setMenuOpen(false)}
                  className={({ isActive }) => cx("flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-nav px-1 py-1 text-[10px] transition-colors duration-fast ease-std", isActive ? "bg-accent-soft font-nav text-action" : "text-ink-muted hover:text-ink")}
                  aria-label={t(MOBILE_LABEL_KEYS[item.to], item.label)}
                  title={t(MOBILE_LABEL_KEYS[item.to], item.label)}
                >
                  <NavIcon name={item.icon} />
                  <span>{t(MOBILE_LABEL_KEYS[item.to], item.label)}</span>
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

          {shortcutsOpen && (
            <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
              <button type="button" className="absolute inset-0 bg-overlay/40" onClick={() => setShortcutsOpen(false)} aria-label="关闭快捷键帮助" />
              <section className="card relative w-full max-w-md p-5" role="dialog" aria-modal="true" aria-label="快捷键帮助"><div className="flex items-center justify-between"><h2>快捷键</h2><button type="button" className="btn h-9 w-9 px-0" onClick={() => setShortcutsOpen(false)}>×</button></div><dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 text-[13px]"><dt><kbd>⌘K</kbd></dt><dd>命令面板</dd><dt><kbd>/</kbd></dt><dd>聚焦域名搜索</dd><dt><kbd>j / k</kbd></dt><dd>列表上下选行</dd><dt><kbd>Enter</kbd></dt><dd>打开选中域名详情</dd><dt><kbd>g</kbd> 然后 <kbd>o/d/w/h/p/s</kbd></dt><dd>跳到 概览 / 域名 / 抢注 / 历史 / 查询源 / 设置</dd><dt><kbd>n</kbd></dt><dd>添加域名</dd><dt><kbd>x</kbd></dt><dd>清空当前选择</dd><dt><kbd>r</kbd></dt><dd>刷新当前数据</dd><dt><kbd>?</kbd></dt><dd>打开本帮助</dd></dl></section>
            </div>
          )}

          <CommandPalette open={commandOpen} onClose={() => setCommandOpen(false)} />

          {/* 墨色反转页脚 —— 整套象牙纸面之后唯一的深色带，给页面一个落地的锚。 */}
          <footer className="footer-inversion mt-12 px-4 py-8 sm:px-6 lg:px-8">
            <div className="mx-auto flex max-w-workbench flex-wrap items-baseline justify-between gap-3">
              <span className="text-[12px] font-semibold uppercase tracking-[0.09em]">DomainHunter</span>
              <span className="footer-meta text-[12px] tracking-[-0.02em]">
                RDAP-first 域名监控与研究工作台 · {version}
              </span>
            </div>
          </footer>
        </div>
      </div>
    </div>
  );
}

function SegmentedControl({ label, value, options, onChange }: { label: string; value: string; options: ReadonlyArray<{ value: string; label: string }>; onChange: (value: string) => void }) {
  // 规范里成本最低、读起来最讲究的动效：滑动的药丸是一个绝对定位的独立元素，
  // 切换选项时它「滑过去」而不是让底色在两个按钮之间闪烁。
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const [thumb, setThumb] = useState({ left: 0, width: 0 });

  useLayoutEffect(() => {
    const wrap = wrapRef.current;
    if (!wrap) return;
    const measure = () => {
      const index = options.findIndex((option) => option.value === value);
      const target = wrap.querySelectorAll<HTMLButtonElement>("[data-seg]")[index < 0 ? 0 : index];
      if (target) setThumb({ left: target.offsetLeft, width: target.offsetWidth });
    };
    measure();
    // 字体加载完成、语言切换、容器换行都会改变按钮宽度，跟着重测一次。
    const ro = new ResizeObserver(measure);
    ro.observe(wrap);
    return () => ro.disconnect();
  }, [value, options]);

  return (
    <fieldset>
      <legend className="sr-only">{label}</legend>
      <div ref={wrapRef} className="segmented" aria-label={label}>
        <span
          className="segmented-thumb"
          style={{ transform: `translateX(${thumb.left}px)`, width: thumb.width, opacity: thumb.width ? 1 : 0 }}
          aria-hidden="true"
        />
        {options.map((option) => (
          <button
            key={option.value}
            data-seg=""
            type="button"
            className="segmented-option"
            onClick={() => onChange(option.value)}
            aria-pressed={option.value === value}
          >
            {option.label}
          </button>
        ))}
      </div>
    </fieldset>
  );
}

function NavItem({ item, onClick, iconOnly }: { item: (typeof NAV)[number]; onClick?: () => void; iconOnly?: boolean }) {
  const { t } = useLocale();
  const label = t(NAV_LABEL_KEYS[item.to], item.label);
  return (
    <NavLink
      to={item.to}
      end={"end" in item ? item.end : undefined}
      onClick={onClick}
      className={({ isActive }) => cx("sidebar-link", iconOnly && "justify-center px-0", isActive && "sidebar-link-active")}
      // 折叠成图标条后，名称也得靠 title 才说得清，不能只留 detail。
      title={iconOnly ? `${label} · ${item.detail}` : item.detail}
      aria-label={iconOnly ? label : undefined}
    >
      <NavIcon name={item.icon} />
      {/* 198px 的侧栏装不下副标题，detail 只留在 title 提示里。 */}
      {!iconOnly && <span className="min-w-0 flex-1 truncate">{label}</span>}
    </NavLink>
  );
}

function BrandBlock({ compact = false }: { compact?: boolean }) {
  return (
    <div className={cx("flex min-w-0 items-center gap-2.5", compact && "gap-2")}>
      {/* 标记直接呈现，不套「圆角方块」—— 那是默认占位的做法。 */}
      <Logo className={cx("shrink-0 text-ink", compact ? "h-5 w-5" : "h-6 w-6")} />
      <div className="min-w-0">
        <div className="brand-wordmark truncate">DomainHunter</div>
        {!compact && <p className="section-label mt-0.5">domain intelligence</p>}
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
