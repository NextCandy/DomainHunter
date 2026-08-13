import { useCallback, useEffect, useState } from "react";
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { api, setCsrfToken } from "./lib/api";
import type { SessionInfo } from "./lib/api";
import { Layout } from "./components/Layout";
import { Spinner, ToastProvider } from "./components/ui";
import { LoginPage } from "./pages/Login";
import { OverviewPage } from "./pages/Overview";
import { DomainsPage } from "./pages/Domains";
import { WatchlistPage } from "./pages/Watchlist";
import { HistoryPage } from "./pages/History";
import { ProvidersPage } from "./pages/Providers";
import { NotificationsPage } from "./pages/Notifications";
import { SettingsPage } from "./pages/Settings";
import { AutomationPage } from "./pages/Automation";

export default function App() {
  const [session, setSession] = useState<SessionInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const navigate = useNavigate();
  const location = useLocation();

  const loadSession = useCallback(async () => {
    try {
      const info = await api.get<SessionInfo>("/api/session");
      setCsrfToken(info.csrf_token ?? "");
      setSession(info);
    } catch {
      setSession({ authenticated: false, auth_required: true });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSession();
  }, [loadSession]);

  useEffect(() => {
    if (!session?.authenticated) return;
    const editable = (target: EventTarget | null) => target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement || (target instanceof HTMLElement && target.isContentEditable);
    const onKey = (event: KeyboardEvent) => {
      if (editable(event.target)) return;
      if (event.key === "/") { event.preventDefault(); navigate("/domains?focus=search"); }
      else if (event.key === "r") {
        event.preventDefault();
        // Pages with a local refresh handler can update in place. The reload
        // fallback keeps the shortcut useful on every route, including pages
        // whose data is composed from several independent resources.
        window.dispatchEvent(new CustomEvent("domainhunter:refresh"));
        window.setTimeout(() => window.location.reload(), 0);
      }
      else if (event.key === "?") { event.preventDefault(); window.dispatchEvent(new CustomEvent("domainhunter:shortcuts")); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navigate, session?.authenticated]);

  const handleUnauthorized = useCallback(() => {
    setSession({ authenticated: false, auth_required: true });
  }, []);

  const handleLogout = useCallback(async () => {
    try {
      await api.post("/api/logout");
    } catch {
      /* 即便请求失败也切回登录页 */
    }
    setCsrfToken("");
    setSession({ authenticated: false, auth_required: true });
    navigate("/login", { replace: true });
  }, [navigate]);

  if (loading) {
    return (
      <div className="flex min-h-full items-center justify-center text-ink-muted">
        <Spinner />
      </div>
    );
  }

  if (!session?.authenticated) {
    return (
      <LoginPage
        onSuccess={() => {
          void loadSession().then(() => {
            if (location.pathname === "/login") navigate("/", { replace: true });
          });
        }}
      />
    );
  }

  return (
    <ToastProvider>
      <Routes>
        <Route
          element={
            <Layout
              username={session.username ?? ""}
              version={session.version ?? ""}
              onLogout={handleLogout}
            />
          }
        >
          <Route path="/" element={<OverviewPage onUnauthorized={handleUnauthorized} />} />
          <Route path="/domains" element={<DomainsPage onUnauthorized={handleUnauthorized} />} />
          <Route path="/watchlist" element={<WatchlistPage onUnauthorized={handleUnauthorized} />} />
          <Route path="/history" element={<HistoryPage onUnauthorized={handleUnauthorized} />} />
          <Route path="/providers" element={<ProvidersPage onUnauthorized={handleUnauthorized} />} />
          <Route
            path="/notifications"
            element={<NotificationsPage onUnauthorized={handleUnauthorized} />}
          />
          <Route
            path="/settings"
            element={
              <SettingsPage
                onUnauthorized={handleUnauthorized}
                onCredentialsChanged={handleUnauthorized}
              />
            }
          />
          <Route path="/automation" element={<AutomationPage onUnauthorized={handleUnauthorized} />} />
          <Route path="/login" element={<Navigate to="/" replace />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </ToastProvider>
  );
}
