import { cx } from "./ui";

/**
 * DomainHunter 的标记是一个**准星**，不是「圆角方块里的首字母」。
 *
 * 含义对应产品本身：RDAP-first 的域名监控就是在一片噪音里锁定目标 ——
 * 外圈是扫描范围、四向刻度是对齐、中心实心点是锁定的那一个域名。
 * 右上象限填强调色、其余描边镂空，让标记自己就带着「唯一强调色」的语法；
 * 悬停时轻微旋转 -8°，像镜头微调了一下。
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={cx(
        "transition-transform duration-slow ease-std hover:-rotate-[8deg]",
        className,
      )}
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      {/* 右上象限：唯一的填色块 */}
      <path d="M12 12V3.2A8.8 8.8 0 0 1 20.8 12Z" className="fill-action" />
      {/* 扫描范围 */}
      <circle
        cx="12"
        cy="12"
        r="8.8"
        stroke="currentColor"
        strokeWidth="1.4"
        opacity="0.9"
      />
      {/* 四向刻度：穿出圆外一截，这是「准星」而不是「圆环」的关键 */}
      <path
        d="M12 0.8v4.1M12 19.1v4.1M0.8 12h4.1M19.1 12h4.1"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
      />
      {/* 锁定的那一个 */}
      <circle cx="12" cy="12" r="1.7" fill="currentColor" />
    </svg>
  );
}
