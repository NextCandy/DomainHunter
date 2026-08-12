import { useState } from "react";
import { cx } from "./ui";

interface RegistrarOption {
  label: string;
  href: (domain: string) => string;
}

const REGISTRARS: RegistrarOption[] = [
  {
    label: "Spaceship",
    href: (domain) => `https://www.spaceship.com/domain-search/cgi-bin/?query=${encodeURIComponent(domain)}`,
  },
  {
    label: "Dynadot",
    href: (domain) => `https://www.dynadot.com/domain/search.html?domain=${encodeURIComponent(domain)}`,
  },
  {
    label: "Namecheap",
    href: (domain) => `https://www.namecheap.com/domains/registration/results/?domain=${encodeURIComponent(domain)}`,
  },
  {
    label: "阿里云",
    href: (domain) => `https://wanwang.aliyun.com/domain/searchresult/#/?keyword=${encodeURIComponent(domain)}`,
  },
  {
    label: "西部数码",
    href: (domain) => `https://www.west.cn/services/domain/?domain=${encodeURIComponent(domain)}`,
  },
];

export function RegistrarLookupMenu({ domain }: { domain: string }) {
  const [open, setOpen] = useState(false);

  return (
    <div className="relative">
      <button
        type="button"
        className="btn h-8"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        注册商查询
        <span aria-hidden="true" className={cx("ml-1 text-[11px] transition-transform", open && "rotate-180")}>
          ▾
        </span>
      </button>
      {open && (
        <div
          className="absolute left-0 top-full z-20 mt-1 min-w-[168px] rounded-md border border-line bg-surface-raised p-1 shadow-lg"
          role="menu"
          aria-label="选择注册商查询"
        >
          {REGISTRARS.map((registrar) => (
            <a
              key={registrar.label}
              className="block rounded px-2.5 py-2 text-[12px] text-ink transition-colors hover:bg-surface-muted"
              href={registrar.href(domain)}
              target="_blank"
              rel="noreferrer"
              role="menuitem"
              onClick={() => setOpen(false)}
            >
              {registrar.label}
            </a>
          ))}
          <p className="border-t border-line px-2.5 pb-1 pt-2 text-[10px] leading-4 text-ink-faint">
            注册状态和价格以注册商实时结果为准
          </p>
        </div>
      )}
    </div>
  );
}
