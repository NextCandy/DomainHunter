import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../lib/api";
import { Logo } from "../components/Logo";
import { Spinner } from "../components/ui";

export function LoginPage({ onSuccess }: { onSuccess: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [remember, setRemember] = useState(true);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      await api.post("/api/login", { username, password, remember });
      onSuccess();
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden bg-canvas px-4 py-10 sm:px-6">
      <div className="design-wash design-wash-coral" aria-hidden="true" />
      <div className="design-wash design-wash-blue" aria-hidden="true" />
      <div className="relative w-full max-w-[460px]">
        <div className="mb-8 flex items-start gap-4">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full border border-ink bg-transparent">
            <Logo className="h-8 w-8" />
          </div>
          <div>
            <p className="workspace-kicker">DOMAIN INTELLIGENCE</p>
            <h1 className="editorial-title mt-2 text-[36px] leading-tight">DomainHunter</h1>
            <p className="mt-2 text-[13px] leading-5 text-ink-muted">域名状态长期监控与研究工作台</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="card bg-periwinkle-mist p-6 sm:p-8">
          <div className="mb-4">
            <label className="label" htmlFor="username">
              用户名
            </label>
            <input
              id="username"
              className="input"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              required
            />
          </div>
          <div className="mb-4">
            <label className="label" htmlFor="password">
              密码
            </label>
            <input
              id="password"
              type="password"
              className="input"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              required
            />
          </div>
          <label className="mb-5 flex items-center gap-2 text-[12px] text-ink-muted">
            <input
              type="checkbox"
              checked={remember}
              onChange={(event) => setRemember(event.target.checked)}
            />
            记住登录状态（30 天）
          </label>

          {error && (
            <p className="mb-3 rounded-[10px] border border-danger/30 bg-blush px-2.5 py-2 text-[12px] text-danger dark:bg-danger/10 dark:text-red-200">
              {error}
            </p>
          )}

          <button type="submit" className="btn btn-primary w-full" disabled={submitting}>
            {submitting && <Spinner />}
            登录 <span aria-hidden="true">→</span>
          </button>
        </form>
      </div>
    </div>
  );
}
