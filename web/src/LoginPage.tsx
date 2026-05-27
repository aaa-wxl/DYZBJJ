import { FormEvent, useMemo, useState } from "react";
import { login, type LoginSession, type Role } from "./api";

type LoginPageProps = {
  defaultRole: Role;
  onLogin: (session: LoginSession) => void;
};

export function LoginPage({ defaultRole, onLogin }: LoginPageProps) {
  const [role, setRole] = useState<Role>(defaultRole);
  const [username, setUsername] = useState(defaultRole === "admin" ? "admin" : "userA");
  const [password, setPassword] = useState(defaultRole === "admin" ? "admin123" : "123456");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const title = useMemo(() => (role === "admin" ? "管理端登录" : "用户端登录"), [role]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      onLogin(await login(username, password));
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  }

  function switchRole(nextRole: Role) {
    setRole(nextRole);
    setUsername(nextRole === "admin" ? "admin" : "userA");
    setPassword(nextRole === "admin" ? "admin123" : "123456");
  }

  return (
    <main className="login-page">
      <section className="login-panel">
        <p className="eyebrow">Realtime Auction</p>
        <h1>登录</h1>
        <p className="login-subtitle">{title}</p>
        <form onSubmit={submit} className="login-form">
          <div className="segmented" role="tablist" aria-label="登录入口">
            <button type="button" className={role === "bidder" ? "active" : ""} onClick={() => switchRole("bidder")}>
              用户端
            </button>
            <button type="button" className={role === "admin" ? "active" : ""} onClick={() => switchRole("admin")}>
              管理端
            </button>
          </div>
          <label>
            用户名
            <input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin / userA / userB / userC" />
          </label>
          <label>
            密码
            <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="输入密码" />
          </label>
          {error && <p className="error-line">{error}</p>}
          <button className="primary-btn" disabled={submitting || !username.trim() || !password.trim()}>
            {submitting ? "登录中" : "进入"}
          </button>
        </form>
      </section>
    </main>
  );
}
