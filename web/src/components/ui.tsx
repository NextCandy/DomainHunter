import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { DomainStatus } from "../lib/api";
import { STATUS_CLASSES, STATUS_LABELS } from "../lib/format";

export function cx(...parts: Array<string | false | undefined | null>): string {
  return parts.filter(Boolean).join(" ");
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
    <section className={cx("card", className)}>
      {(title || action) && (
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-2.5">
          <h2 className="text-[13px] font-semibold text-ink">{title}</h2>
          {action}
        </header>
      )}
      <div className={cx("p-4", bodyClassName)}>{children}</div>
    </section>
  );
}

export function StatusBadge({ status, className }: { status: DomainStatus; className?: string }) {
  return (
    <span
      className={cx(
        "inline-flex items-center whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] font-medium leading-4",
        STATUS_CLASSES[status] ?? STATUS_CLASSES.unknown,
        className,
      )}
    >
      {STATUS_LABELS[status] ?? status}
    </span>
  );
}

export function Pill({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span
      className={cx(
        "inline-flex items-center rounded border border-line bg-surface-muted px-1.5 py-0.5 text-[11px] leading-4 text-ink-muted",
        className,
      )}
    >
      {children}
    </span>
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

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-1 py-10 text-center">
      <p className="text-[13px] text-ink-muted">{title}</p>
      {hint && <p className="text-[12px] text-ink-faint">{hint}</p>}
    </div>
  );
}

export function ErrorNotice({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-[13px] text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
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
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-40">
      <div
        className="absolute inset-0 bg-black/40"
        onClick={onClose}
        role="presentation"
        aria-hidden="true"
      />
      <aside
        className="absolute inset-y-0 right-0 flex w-full max-w-[560px] flex-col bg-surface-raised shadow-xl"
        role="dialog"
        aria-modal="true"
      >
        <header className="flex items-start justify-between gap-3 border-b border-line px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-[15px] font-semibold text-ink">{title}</h2>
            {subtitle && <div className="mt-0.5 text-[12px] text-ink-muted">{subtitle}</div>}
          </div>
          <button type="button" className="btn btn-ghost h-7 px-2" onClick={onClose} aria-label="关闭">
            ✕
          </button>
        </header>
        <div className="flex-1 overflow-y-auto overscroll-contain p-4">{children}</div>
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
      <div className="card relative w-full max-w-sm p-4" role="dialog" aria-modal="true">
        <h3 className="text-[14px] font-semibold text-ink">{title}</h3>
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

type Toast = { id: number; message: string; tone: "info" | "error" | "success" };
type ToastContextValue = (message: string, tone?: Toast["tone"]) => void;

const ToastContext = createContext<ToastContextValue>(() => {});

export function useToast() {
  return useContext(ToastContext);
}

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(1);

  const push: ToastContextValue = (message, tone = "info") => {
    const id = nextId.current++;
    setToasts((current) => [...current, { id, message, tone }]);
    window.setTimeout(() => {
      setToasts((current) => current.filter((item) => item.id !== id));
    }, 4200);
  };

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-full max-w-xs flex-col gap-2">
        {toasts.map((toast) => (
          <div
            key={toast.id}
            className={cx(
              "pointer-events-auto rounded-md border px-3 py-2 text-[13px] shadow-sm",
              toast.tone === "error"
                ? "border-red-200 bg-red-50 text-red-700 dark:border-red-500/30 dark:bg-red-500/15 dark:text-red-200"
                : toast.tone === "success"
                  ? "border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-500/30 dark:bg-emerald-500/15 dark:text-emerald-200"
                  : "border-line bg-surface-raised text-ink",
            )}
          >
            {toast.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
