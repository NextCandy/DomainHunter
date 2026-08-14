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
  const current = ROUTE_KEYS[pathname] ?? "breadcrumb.home";
  return (
    <nav className="breadcrumb" aria-label="面包屑">
      {pathname === "/" ? (
        <span aria-current="page" className="truncate text-ink-muted">{t(current)}</span>
      ) : (
        <>
          <Link to="/" aria-label={t("breadcrumb.home")}>{t("breadcrumb.home")}</Link>
          <span aria-hidden="true">/</span>
          <span aria-current="page" className="truncate text-ink-muted">{t(current)}</span>
        </>
      )}
    </nav>
  );
}
