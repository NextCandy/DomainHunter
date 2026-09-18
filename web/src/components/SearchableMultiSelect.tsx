import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { cx, useIsMobile } from "./ui";

export interface SelectOption {
  value: string;
  /** 列表和触发按钮上显示的短名。 */
  label: string;
  /** 命中数量，显示在选项右侧。 */
  count?: number;
  /** 完整名称，挂在 title 上——短名认品牌，全名留给需要核对的人。 */
  title?: string;
  /** 参与搜索但不显示的额外关键词（例如注册商全名）。 */
  keywords?: string;
}

interface Props {
  options: SelectOption[];
  /** 已选值；空数组表示"全部"。 */
  selected: string[];
  onChange: (next: string[]) => void;
  /** 未选任何项时触发按钮上的文案，例如"全部注册商"。 */
  label: string;
  searchPlaceholder?: string;
  /** 单选模式下选中即关闭，且最多保留一个值。 */
  multiple?: boolean;
  className?: string;
  /** 过滤后最多渲染多少条。500+ 的域名列表一次性渲染会拖慢打开速度。 */
  maxRender?: number;
  disabled?: boolean;
}

export function SearchableMultiSelect({
  options,
  selected,
  onChange,
  label,
  searchPlaceholder = "输入以筛选…",
  multiple = true,
  className,
  maxRender = 300,
  disabled,
}: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();
  const isMobile = useIsMobile();

  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (!needle) return options;
    return options.filter(
      (option) =>
        option.label.toLowerCase().includes(needle) ||
        option.value.toLowerCase().includes(needle) ||
        (option.keywords ?? "").toLowerCase().includes(needle),
    );
  }, [options, query]);

  const visible = useMemo(() => filtered.slice(0, maxRender), [filtered, maxRender]);
  const hiddenCount = filtered.length - visible.length;

  const close = useCallback(() => {
    setOpen(false);
    setQuery("");
    setActiveIndex(0);
  }, []);

  // 打开后把焦点交给搜索框：这个控件的主交互是"输入以缩小范围"。
  useEffect(() => {
    if (!open) return;
    const frame = window.requestAnimationFrame(() => searchRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [open]);

  useEffect(() => setActiveIndex(0), [query]);

  // 点击面板外关闭。移动端是全屏 sheet，由自身的遮罩处理，不需要这条。
  useEffect(() => {
    if (!open || isMobile) return;
    const onPointerDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) close();
    };
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [close, isMobile, open]);

  // 焦点陷阱：Tab 在面板内循环，不会跑到背后的表格里去。
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Tab") return;
      const panel = panelRef.current;
      if (!panel) return;
      const focusables = panel.querySelectorAll<HTMLElement>(
        'input, button:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      if (focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [open]);

  // 键盘移动时把高亮项滚进视野。
  useEffect(() => {
    if (!open) return;
    const node = listRef.current?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`);
    node?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, open]);

  function toggle(value: string) {
    if (!multiple) {
      onChange(selectedSet.has(value) ? [] : [value]);
      close();
      return;
    }
    const next = new Set(selectedSet);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    onChange(Array.from(next));
  }

  function onSearchKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex((current) => Math.min(visible.length - 1, current + 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex((current) => Math.max(0, current - 1));
    } else if (event.key === "Enter") {
      event.preventDefault();
      const option = visible[activeIndex];
      if (option) toggle(option.value);
    } else if (event.key === "Escape") {
      event.preventDefault();
      close();
    }
  }

  const summary = useMemo(() => {
    if (selected.length === 0) return label;
    const first = options.find((option) => option.value === selected[0]);
    const firstLabel = first?.label ?? selected[0];
    return selected.length === 1 ? firstLabel : `${firstLabel} +${selected.length - 1}`;
  }, [label, options, selected]);

  const list = (
    <div
      ref={listRef}
      className={cx("min-w-0 overflow-y-auto overscroll-contain", isMobile ? "flex-1" : "max-h-[min(340px,50vh)]")}
      role="listbox"
      id={listboxId}
      aria-multiselectable={multiple}
      aria-label={label}
    >
      {visible.length === 0 ? (
        <p className="px-3 py-6 text-center text-[12px] text-ink-faint">没有匹配项</p>
      ) : (
        visible.map((option, index) => {
          const active = index === activeIndex;
          const checked = selectedSet.has(option.value);
          return (
            <button
              key={option.value}
              type="button"
              role="option"
              aria-selected={checked}
              data-index={index}
              className={cx(
                "flex w-full min-w-0 items-center gap-2 px-3 text-left text-[12px] text-ink",
                isMobile ? "min-h-11" : "min-h-9",
                active && "bg-surface-muted",
                checked && "text-accent",
              )}
              title={option.title ?? option.label}
              onMouseEnter={() => setActiveIndex(index)}
              onClick={() => toggle(option.value)}
            >
              <span
                aria-hidden="true"
                className={cx(
                  "flex h-3.5 w-3.5 shrink-0 items-center justify-center border text-[10px] leading-none",
                  multiple ? "rounded-[3px]" : "rounded-full",
                  checked ? "border-accent bg-accent text-on-accent" : "border-line",
                )}
              >
                {checked ? "✓" : ""}
              </span>
              <span className="min-w-0 flex-1 truncate">{option.label}</span>
              {option.count !== undefined && (
                <span className="tabular shrink-0 text-[11px] text-ink-faint">{option.count}</span>
              )}
            </button>
          );
        })
      )}
      {hiddenCount > 0 && (
        <p className="border-t border-line px-3 py-2 text-[11px] text-ink-faint">
          还有 {hiddenCount} 项未显示，继续输入以缩小范围
        </p>
      )}
    </div>
  );

  const header = (
    <div className={cx("flex items-center gap-2 border-b border-line p-2", isMobile && "p-3")}>
      <input
        ref={searchRef}
        className="input min-w-0 flex-1"
        placeholder={searchPlaceholder}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={onSearchKeyDown}
        aria-controls={listboxId}
        aria-autocomplete="list"
        aria-activedescendant={undefined}
      />
      {isMobile && (
        <button type="button" className="btn min-h-11 shrink-0" onClick={close}>
          完成
        </button>
      )}
    </div>
  );

  const footer = selected.length > 0 && (
    <div className="flex items-center justify-between gap-2 border-t border-line px-3 py-2 text-[11px] text-ink-muted">
      <span>已选 {selected.length} 项</span>
      <button
        type="button"
        className="min-h-8 px-2 text-accent hover:underline"
        onClick={() => {
          onChange([]);
          if (!multiple) close();
        }}
      >
        清空
      </button>
    </div>
  );

  return (
    <div ref={rootRef} className={cx("relative min-w-0", className)}>
      <button
        type="button"
        className={cx(
          "input flex items-center justify-between gap-2 text-left",
          selected.length > 0 && "border-accent text-accent",
        )}
        onClick={() => (open ? close() : setOpen(true))}
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-label={selected.length > 0 ? `${label}：已选 ${selected.length} 项` : label}
        title={summary}
        disabled={disabled}
      >
        <span className="min-w-0 truncate">{summary}</span>
        <span aria-hidden="true" className="shrink-0 text-ink-faint">
          ⌄
        </span>
      </button>

      {open && !isMobile && (
        <div
          ref={panelRef}
          className="absolute left-0 top-[calc(100%+4px)] z-40 w-[max(100%,260px)] rounded-card border border-line bg-surface-raised shadow-lg"
        >
          {header}
          {list}
          {footer}
        </div>
      )}

      {open && isMobile && (
        <div className="fixed inset-0 z-50 flex flex-col">
          <div className="absolute inset-0 bg-overlay/40" onClick={close} role="presentation" aria-hidden="true" />
          <div
            ref={panelRef}
            className="relative mt-auto flex h-[85dvh] flex-col rounded-t-card border border-line bg-surface-raised"
            role="dialog"
            aria-modal="true"
            aria-label={label}
          >
            <div className="flex items-center justify-between gap-2 border-b border-line px-3 py-3">
              <h3 className="min-w-0 truncate text-[14px] text-ink">{label}</h3>
              <button
                type="button"
                className="btn btn-ghost min-h-11 min-w-11 px-0"
                onClick={close}
                aria-label="关闭筛选"
              >
                ×
              </button>
            </div>
            {header}
            {list}
            {footer}
          </div>
        </div>
      )}
    </div>
  );
}
