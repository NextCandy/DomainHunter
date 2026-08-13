import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ReactNode } from "react";
import type { DomainStatus } from "../lib/api";
import { hasTransferLock, STATUS_CLASSES, STATUS_LABELS } from "../lib/format";

export function cx(...parts: Array<string | false | undefined | null>): string {
  return parts.filter(Boolean).join(" ");
}

export type DensityMode = "comfortable" | "compact" | "dense";

const DENSITY_STORAGE_KEY = "dh-density";

interface DensityContextValue {
  density: DensityMode;
  setDensity: (density: DensityMode) => void;
}

export const DensityContext = createContext<DensityContextValue>({
  density: "comfortable",
  setDensity: () => {},
});

export function useDensity() {
  return useContext(DensityContext);
}

function readDensity(): DensityMode {
  try {
    const stored = localStorage.getItem(DENSITY_STORAGE_KEY);
    if (stored === "comfortable" || stored === "compact" || stored === "dense") return stored;
  } catch {
    /* localStorage 不可用时使用默认密度 */
  }
  return "comfortable";
}

export function DensityProvider({ children }: { children: ReactNode }) {
  const [density, setDensityState] = useState<DensityMode>(readDensity);

  useEffect(() => {
    document.documentElement.dataset.density = density;
  }, [density]);

  const setDensity = useCallback((next: DensityMode) => {
    setDensityState(next);
    try {
      localStorage.setItem(DENSITY_STORAGE_KEY, next);
    } catch {
      /* 忽略存储失败，内存中的设置仍然有效 */
    }
  }, []);

  const value = useMemo(() => ({ density, setDensity }), [density, setDensity]);
  return <DensityContext.Provider value={value}>{children}</DensityContext.Provider>;
}

/** 当列表刷新后某个域名的状态发生变化时，给它一个短暂的视觉提示。 */
export function useStatusChangeHighlights<T>(
  items: T[],
  getKey: (item: T) => string,
  getStatus: (item: T) => string,
) {
  const previous = useRef<Map<string, string> | null>(null);
  const getKeyRef = useRef(getKey);
  const getStatusRef = useRef(getStatus);
  const [highlighted, setHighlighted] = useState<Set<string>>(new Set());

  getKeyRef.current = getKey;
  getStatusRef.current = getStatus;

  useEffect(() => {
    const next = new Map(items.map((item) => [getKeyRef.current(item), getStatusRef.current(item)]));
    const previousValues = previous.current;
    previous.current = next;
    if (!previousValues) return;

    const changed = new Set<string>();
    next.forEach((status, key) => {
      if (previousValues.get(key) && previousValues.get(key) !== status) changed.add(key);
    });
    if (changed.size === 0) return;

    setHighlighted(changed);
    const timer = window.setTimeout(() => setHighlighted(new Set()), 800);
    return () => window.clearTimeout(timer);
  }, [items]);

  return highlighted;
}

export function Card({
  title,
  action,
  children,
  className,
  bodyClassName,
}: {
  title?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
}) {
  return (
    <section className={cx("card min-w-0", className)}>
      {(title || action) && (
        <header className="density-card-header flex min-w-0 items-center justify-between gap-3 border-b border-line px-6 py-4">
          <h2 className="min-w-0 truncate font-display text-[20px] font-normal tracking-[-0.02em] text-ink">{title}</h2>
          {action}
        </header>
      )}
      <div className={cx("density-card-body min-w-0 p-6", bodyClassName)}>{children}</div>
    </section>
  );
}

export function StatusBadge({
  status,
  eppStatuses,
  className,
}: {
  status: DomainStatus;
  eppStatuses?: string[] | null;
  className?: string;
}) {
  const transferLocked = status === "transfer_locked" || hasTransferLock(eppStatuses);
  return (
    <span
      className={cx(
        "inline-flex items-center gap-1 whitespace-nowrap rounded-tag border px-2.5 py-1 text-[10px] font-medium uppercase leading-4 tracking-[-0.02em]",
        STATUS_CLASSES[status] ?? STATUS_CLASSES.unknown,
        className,
      )}
      title={transferLocked ? "转移锁定" : undefined}
    >
      {STATUS_LABELS[status] ?? status}
      {transferLocked && <TransferLockMark />}
    </span>
  );
}

function TransferLockMark() {
  return (
    <svg
      aria-hidden="true"
      className="h-3 w-3 shrink-0"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="3.25" y="7" width="9.5" height="6.25" rx="1.25" />
      <path d="M5.25 7V5.25a2.75 2.75 0 0 1 5.5 0V7" />
    </svg>
  );
}

export function Pill({
  children,
  className,
  title,
}: {
  children: ReactNode;
  className?: string;
  title?: string;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center rounded-tag border border-line bg-transparent px-2.5 py-1 text-[10px] uppercase leading-4 tracking-[-0.02em] text-ink-muted",
        className,
      )}
      title={title}
    >
      {children}
    </span>
  );
}

export function DomainName({
  name,
  favorite,
  onClick,
  className,
}: {
  name: string;
  favorite?: boolean;
  onClick?: () => void;
  className?: string;
}) {
  const content = (
    <>
      {favorite && (
        <span className="mr-1 text-accent" aria-hidden="true">
          ★
        </span>
      )}
      <span className="block min-w-0 truncate whitespace-nowrap">{name}</span>
    </>
  );

  if (onClick) {
    return (
      <button
        type="button"
        className={cx("mono block min-w-0 max-w-full truncate whitespace-nowrap text-left text-ink hover:text-accent hover:underline", className)}
        onClick={onClick}
        title={name}
      >
        {content}
      </button>
    );
  }

  return <span className={cx("mono block min-w-0 max-w-full truncate whitespace-nowrap", className)} title={name}>{content}</span>;
}

export function ReviewIndicator({ explanation }: { explanation?: string }) {
  return (
    <span
      className="review-dot"
      role="img"
      aria-label="需复核"
      title={explanation || "查询事实或证据需要复核"}
    />
  );
}

export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cx(
        "inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-r-transparent align-[-2px]",
        className,
      )}
      role="status"
      aria-label="加载中"
    />
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <span className={cx("skeleton block rounded", className)} aria-hidden="true" />;
}

export function EmptyState({
  title,
  hint,
  action,
}: {
  title: string;
  hint?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-1 px-4 py-10 text-center">
      <span
        className="mb-2 flex h-10 w-10 items-center justify-center rounded-full border border-line bg-transparent font-display text-[18px] text-ink-faint"
        aria-hidden="true"
      >
        ∅
      </span>
      <p className="text-[13px] text-ink-muted">{title}</p>
      {hint && <p className="text-[12px] text-ink-faint">{hint}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function ErrorNotice({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-surface-muted px-4 py-3 text-[12px] text-ink-muted">
      <span>{message}</span>
      {onRetry && (
        <button type="button" className="btn btn-ghost h-7 px-2 text-[12px]" onClick={onRetry}>
          重试
        </button>
      )}
    </div>
  );
}

/** 右侧抽屉：桌面端从右侧滑出，移动端占满宽度 */
export function Drawer({
  open,
  onClose,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  children: ReactNode;
}) {
  const [mounted, setMounted] = useState(open);
  const [visible, setVisible] = useState(open);

  useEffect(() => {
    if (open) {
      setMounted(true);
      const frame = window.requestAnimationFrame(() => setVisible(true));
      return () => window.cancelAnimationFrame(frame);
    }

    setVisible(false);
    const timer = window.setTimeout(() => setMounted(false), 220);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    if (!mounted) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [mounted, onClose]);

  if (!mounted) return null;
  return (
    <div className="fixed inset-0 z-50">
      <div
        className={cx(
          "absolute inset-0 bg-black/40 transition-opacity duration-200 motion-reduce:transition-none",
          visible ? "opacity-100" : "opacity-0",
        )}
        onClick={onClose}
        role="presentation"
        aria-hidden="true"
      />
      <aside
        className={cx(
          "absolute inset-x-0 bottom-0 flex max-h-[92vh] w-full flex-col rounded-t-card border border-line bg-surface-raised transition-transform duration-200 ease-out motion-reduce:transition-none",
          "lg:inset-y-0 lg:bottom-auto lg:left-auto lg:right-0 lg:max-h-none lg:w-[min(560px,92vw)] lg:rounded-none",
          visible ? "translate-y-0 lg:translate-x-0" : "translate-y-full lg:translate-y-0 lg:translate-x-full",
        )}
        role="dialog"
        aria-modal="true"
      >
        <header className="flex items-start justify-between gap-3 border-b border-line px-6 py-5">
          <div className="min-w-0">
            <h2 className="truncate font-display text-[24px] font-normal tracking-[-0.02em] text-ink">{title}</h2>
            {subtitle && <div className="mt-0.5 text-[12px] text-ink-muted">{subtitle}</div>}
          </div>
          <button
            type="button"
            className="btn btn-ghost h-7 w-7 px-0"
            onClick={onClose}
            aria-label="关闭抽屉"
            title="关闭抽屉"
          >
            <svg
              aria-hidden="true"
              className="h-4 w-4"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            >
              <path d="m4 4 8 8M12 4l-8 8" />
            </svg>
          </button>
        </header>
        <div className="flex-1 overflow-y-auto overscroll-contain p-6">{children}</div>
      </aside>
    </div>
  );
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmText = "确认",
  danger,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description?: string;
  confirmText?: string;
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/40" onClick={onCancel} role="presentation" />
      <div className="card relative w-full max-w-sm p-6" role="dialog" aria-modal="true">
        <h3 className="font-display text-[24px] font-normal tracking-[-0.02em] text-ink">{title}</h3>
        {description && <p className="mt-1.5 text-[13px] text-ink-muted">{description}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="btn" onClick={onCancel}>
            取消
          </button>
          <button
            type="button"
            className={cx("btn", danger ? "btn-danger" : "btn-primary")}
            onClick={onConfirm}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  );
}

// ---------- 轻量 Toast ----------

type Toast = { id: number; message: string; tone: "info" | "error" | "success"; leaving?: boolean };
type ToastContextValue = (message: string, tone?: Toast["tone"]) => void;

const ToastContext = createContext<ToastContextValue>(() => {});

export function useToast() {
  return useContext(ToastContext);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((item) => item.id !== id));
  }, []);

  const push = useCallback<ToastContextValue>((message, tone = "info") => {
    const id = nextId.current++;
    setToasts((current) => [...current, { id, message, tone }]);
    window.setTimeout(() => {
      setToasts((current) =>
        current.map((item) => (item.id === id ? { ...item, leaving: true } : item)),
      );
    }, 3900);
    window.setTimeout(() => dismiss(id), 4220);
  }, [dismiss]);

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-[calc(100%-2rem)] max-w-xs flex-col gap-2"
        role="region"
        aria-label="通知"
      >
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={cx(
              "toast pointer-events-auto flex items-start gap-2 rounded-card border px-3 py-2 text-[13px]",
              toast.leaving ? "toast-leave" : "toast-enter",
              toast.tone === "info"
                ? "border-accent/30 bg-accent-soft text-ink"
                : "border-line bg-surface-muted text-ink-muted",
            )}
            role="status"
            aria-live="polite"
          >
            <span className="min-w-0 flex-1">{toast.message}</span>
            <button
              type="button"
              className="btn btn-ghost -mr-1 -mt-1 h-6 w-6 shrink-0 px-0"
              onClick={() => dismiss(toast.id)}
              aria-label="关闭提示"
              title="关闭提示"
            >
              <svg
                aria-hidden="true"
                className="h-3.5 w-3.5"
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
              >
                <path d="m4 4 8 8M12 4l-8 8" />
              </svg>
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export interface SparklineSeries {
  label: string;
  values: number[];
  color: string;
}

export function Sparkline({ series, labels, className }: { series: SparklineSeries[]; labels?: string[]; className?: string }) {
  const width = 320;
  const height = 92;
  const padding = 8;
  const count = Math.max(2, ...series.map((item) => item.values.length));
  const max = Math.max(1, ...series.flatMap((item) => item.values));

  const makePath = (values: number[]) =>
    values
      .map((value, index) => {
        const x = padding + (index / Math.max(1, count - 1)) * (width - padding * 2);
        const y = height - padding - (value / max) * (height - padding * 2);
        return `${index === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`;
      })
      .join(" ");

  return (
    <div className={cx("min-w-0", className)}>
      <svg
        className="h-24 w-full overflow-visible"
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label="最近七天趋势图"
        preserveAspectRatio="none"
      >
        {[0, 0.5, 1].map((ratio) => {
          const y = padding + ratio * (height - padding * 2);
          return <path key={ratio} d={`M ${padding} ${y} H ${width - padding}`} stroke="rgb(var(--line))" strokeDasharray="2 4" />;
        })}
        {series.map((item) => (
          <path
            key={item.label}
            d={makePath(item.values)}
            fill="none"
            stroke={item.color}
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        ))}
        {Array.from({ length: count }, (_, index) => {
          const x = padding + (index / Math.max(1, count - 1)) * (width - padding * 2);
          return (
            <g key={index}>
              <line x1={x} y1={padding} x2={x} y2={height - padding} stroke="transparent" strokeWidth="20">
                <title>{`${labels?.[index] ?? `第 ${index + 1} 天`} · ${series.map((item) => `${item.label} ${item.values[index] ?? 0}`).join(" · ")}`}</title>
              </line>
              {series.map((item) => {
                const value = item.values[index] ?? 0;
                const y = height - padding - (value / max) * (height - padding * 2);
                return <circle key={item.label} cx={x} cy={y} r="2.5" fill={item.color}><title>{`${labels?.[index] ?? ""} · ${item.label} ${value}`}</title></circle>;
              })}
            </g>
          );
        })}
      </svg>
      <div className="mt-1 flex justify-between text-[10px] text-ink-faint"><span>{max}</span><span>Y 轴：次数</span><span>0</span></div>
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-ink-muted">
        {series.map((item) => (
          <span key={item.label} className="inline-flex items-center gap-1">
            <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: item.color }} aria-hidden="true" />
            {item.label}
          </span>
        ))}
      </div>
    </div>
  );
}
