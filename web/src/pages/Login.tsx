import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../lib/api";
import { Logo } from "../components/Logo";
import { Spinner } from "../components/ui";
import { AuroraBackground } from "../components/AuroraBackground";

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
    <div className="relative flex min-h-full items-center justify-center overflow-hidden px-4 py-10 sm:px-6">
      {/* 登录页也挂氛围层，和工作台是同一片灯光（原先那两个 design-wash 早被 CSS 隐藏了）。 */}
      <AuroraBackground />
      <div className="relative w-full max-w-[460px]">
        <div className="mb-8 flex items-start gap-3.5">
          {/* 标记直接呈现，不套方框。 */}
          <Logo className="mt-0.5 h-9 w-9 shrink-0 text-ink" />
          <div>
            <p className="workspace-kicker">DOMAIN INTELLIGENCE</p>
            <h1 className="editorial-title mt-2">DomainHunter</h1>
            <p className="editorial-lede mt-2">域名状态长期监控与研究工作台</p>
          </div>
        </div>

        <form onSubmit={handleSubmit} className="card p-6 sm:p-8">
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
            <p className="mb-3 rounded-[8px] border border-danger/30 bg-danger/8 px-2.5 py-2 text-[12px] text-ink">
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
