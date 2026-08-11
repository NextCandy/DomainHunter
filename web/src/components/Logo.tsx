/**
 * DomainHunter 的标识：准星环锁定中心的一个点。
 *
 * 环 + 四个刻度是"盯住目标"，中心那个蓝点就是域名里的那个点。
 * 颜色全部走主题变量，浅色 / 深色下自动反相，不需要两套图。
 * 与 web/index.html 里的 favicon 是同一套几何，改一处要同步改另一处。
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true" focusable="false">
      <rect width="32" height="32" rx="7" className="fill-ink" />
      <g fill="none" className="stroke-surface-raised" strokeWidth={2} strokeLinecap="round">
        <circle cx="16" cy="16" r="8.25" />
        <path d="M16 2.5v2.5M16 27v2.5M2.5 16h2.5M27 16h2.5" />
      </g>
      <circle cx="16" cy="16" r="3.25" className="fill-accent" />
    </svg>
  );
}
