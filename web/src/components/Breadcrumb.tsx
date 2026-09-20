import { Link, useLocation } from "react-router-dom";
import { useLocale } from "../lib/i18n";

const ROUTE_KEYS: Record<string, string> = {
  "/": "breadcrumb.home",
  "/domains": "breadcrumb.domains",
  "/watchlist": "breadcrumb.watchlist",
  "/history": "breadcrumb.history",
  "/providers": "breadcrumb.providers",
  "/notifications": "breadcrumb.notifications",
  "/automation": "breadcrumb.automation",
  "/settings": "breadcrumb.settings",
};

export function Breadcrumb() {
  const { pathname } = useLocation();
  const { t } = useLocale();

  // /domains/:name 这类深链不在静态表里，落回默认值会显示成"概览 / 概览"。
  // 单独认一下：概览 / 域名资产 / <域名>。
  const domainDetail = pathname.startsWith("/domains/")
    ? decodeURIComponent(pathname.slice("/domains/".length))
    : "";

  const current = ROUTE_KEYS[pathname] ?? "breadcrumb.home";
  return (
    <nav className="breadcrumb" aria-label="面包屑">
      {pathname === "/" ? (
        <span aria-current="page" className="truncate text-ink-muted">{t(current)}</span>
      ) : (
        <>
          <Link to="/" aria-label={t("breadcrumb.home")}>{t("breadcrumb.home")}</Link>
          <span aria-hidden="true">/</span>
          {domainDetail ? (
            <>
              <Link to="/domains">{t("breadcrumb.domains")}</Link>
              <span aria-hidden="true">/</span>
              <span aria-current="page" className="mono truncate text-ink-muted">{domainDetail}</span>
            </>
          ) : (
            <span aria-current="page" className="truncate text-ink-muted">{t(current)}</span>
          )}
        </>
      )}
    </nav>
  );
}
