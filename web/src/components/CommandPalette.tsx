import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useLocale } from "../lib/i18n";
import { cx, useDensity } from "./ui";

type Command = { id: string; label: string; hint: string; group: string; run: () => void };

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const location = useLocation();
  const { locale, setLocale, t } = useLocale();
  const { setDensity } = useDensity();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const commands = useMemo<Command[]>(() => {
    const go = (path: string) => { navigate(path); onClose(); };
    return [
      { id: "overview", label: t("nav.overview"), hint: "g o", group: "导航", run: () => go("/") },
      { id: "domains", label: t("nav.domains"), hint: "g d", group: "导航", run: () => go("/domains") },
      { id: "watchlist", label: t("nav.watchlist"), hint: "g w", group: "导航", run: () => go("/watchlist") },
      { id: "history", label: t("nav.history"), hint: "g h", group: "导航", run: () => go("/history") },
      { id: "providers", label: t("nav.providers"), hint: "g p", group: "导航", run: () => go("/providers") },
      { id: "settings", label: t("nav.settings"), hint: "g s", group: "导航", run: () => go("/settings") },
      { id: "search", label: "聚焦域名搜索", hint: "/", group: "操作", run: () => go("/domains?focus=search") },
      { id: "refresh", label: "刷新当前数据", hint: "r", group: "操作", run: () => { window.dispatchEvent(new CustomEvent("domainhunter:refresh")); onClose(); } },
      { id: "density-compact", label: "使用紧凑密度", hint: "", group: "偏好", run: () => { setDensity("compact"); onClose(); } },
      { id: "density-comfortable", label: "使用宽松密度", hint: "", group: "偏好", run: () => { setDensity("comfortable"); onClose(); } },
      { id: "locale", label: locale === "zh-CN" ? "切换到 English" : "切换到中文", hint: "", group: "偏好", run: () => { setLocale(locale === "zh-CN" ? "en-US" : "zh-CN"); onClose(); } },
      { id: "current", label: `当前页面：${location.pathname}`, hint: "", group: "上下文", run: onClose },
    ];
  }, [locale, location.pathname, navigate, onClose, setDensity, setLocale, t]);

  const filtered = commands.filter((command) => `${command.label} ${command.hint} ${command.group}`.toLowerCase().includes(query.toLowerCase()));

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActive(0);
    const frame = window.requestAnimationFrame(() => inputRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); onClose(); }
      if (event.key === "ArrowDown") { event.preventDefault(); setActive((index) => Math.min(filtered.length - 1, index + 1)); }
      if (event.key === "ArrowUp") { event.preventDefault(); setActive((index) => Math.max(0, index - 1)); }
      if (event.key === "Enter" && filtered[active]) { event.preventDefault(); filtered[active].run(); }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [active, filtered, onClose, open]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-[70] flex items-start justify-center bg-overlay/35 p-4 pt-[min(18vh,9rem)]" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="w-full max-w-xl overflow-hidden rounded-card border border-line bg-surface-raised shadow-2xl" role="dialog" aria-modal="true" aria-label={t("common.command")}>
        <div className="flex items-center gap-3 border-b border-line px-4">
          <SearchIcon />
          <input ref={inputRef} className="h-14 min-w-0 flex-1 bg-transparent text-[14px] text-ink outline-none placeholder:text-ink-faint" value={query} onChange={(event) => { setQuery(event.target.value); setActive(0); }} placeholder={t("common.search")} aria-label={t("common.search")} />
          <kbd>Esc</kbd>
        </div>
        <div className="max-h-[min(60vh,30rem)] overflow-y-auto p-2" role="listbox" aria-label={t("common.command")}>
          {filtered.length === 0 ? <p className="px-3 py-8 text-center text-[13px] text-ink-muted">没有匹配的命令</p> : filtered.map((command, index) => (
            <button key={command.id} type="button" role="option" aria-selected={index === active} className={cx("flex min-h-11 w-full items-center gap-3 rounded-md px-3 text-left text-[13px]", index === active ? "bg-accent-soft/60 text-ink" : "text-ink-muted hover:bg-surface-muted hover:text-ink")} onMouseEnter={() => setActive(index)} onClick={command.run}>
              <span className="min-w-0 flex-1 truncate">{command.label}<span className="ml-2 text-[10px] text-ink-faint">{command.group}</span></span>
              {command.hint && <kbd>{command.hint}</kbd>}
            </button>
          ))}
        </div>
        <div className="flex items-center justify-between border-t border-line px-4 py-2 text-[10px] text-ink-faint"><span>↑↓ 选择 · Enter 执行</span><span>⌘/Ctrl K</span></div>
      </section>
    </div>
  );
}

function SearchIcon() { return <svg aria-hidden="true" className="h-4 w-4 shrink-0 text-ink-faint" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"><circle cx="7" cy="7" r="3.75" /><path d="m10 10 3.25 3.25" /></svg>; }
