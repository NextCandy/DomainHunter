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
    <div className="flex min-h-full items-center justify-center px-4 py-10">
      <div className="w-full max-w-[340px]">
        <div className="mb-6 flex items-center gap-2">
          <Logo className="h-7 w-7" />
          <div>
            <h1 className="text-[16px] font-semibold tracking-tight">DomainHunter</h1>
            <p className="text-[12px] text-ink-muted">域名状态长期监控</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="card p-4">
          <div className="mb-3">
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
          <div className="mb-3">
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
          <label className="mb-4 flex items-center gap-2 text-[12px] text-ink-muted">
            <input
              type="checkbox"
              checked={remember}
              onChange={(event) => setRemember(event.target.checked)}
            />
            记住登录状态（30 天）
          </label>

          {error && (
            <p className="mb-3 rounded-md border border-red-200 bg-red-50 px-2.5 py-1.5 text-[12px] text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
              {error}
            </p>
          )}

          <button type="submit" className="btn btn-primary w-full" disabled={submitting}>
            {submitting && <Spinner />}
            登录
          </button>
        </form>
      </div>
    </div>
  );
}
