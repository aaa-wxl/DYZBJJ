import { FormEvent, useMemo, useState } from "react";
import { login, type LoginSession, type Role } from "./api";

type LoginPageProps = {
  defaultRole: Role;
  onLogin: (session: LoginSession) => void;
};

export function LoginPage({ defaultRole, onLogin }: LoginPageProps) {
  const [role, setRole] = useState<Role>(defaultRole);
  const [name, setName] = useState(defaultRole === "admin" ? "管理员" : "用户A");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const title = useMemo(() => (role === "admin" ? "管理端登录" : "用户端登录"), [role]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      onLogin(await login(name, role));
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setSubmitting(false);
    }
  }

  function switchRole(nextRole: Role) {
    setRole(nextRole);
    setName(nextRole === "admin" ? "管理员" : "用户A");
  }

  return (
    <main className="login-page">
      <section className="login-visual" aria-hidden="true">
        <div className="login-price">￥3,000</div>
        <div className="login-lot">
          <span>RUNNING</span>
          <strong>星河翡翠手镯</strong>
        </div>
      </section>
      <section className="login-panel">
        <p className="eyebrow">Realtime Auction</p>
        <h1>{title}</h1>
        <form onSubmit={submit} className="login-form">
          <div className="segmented" role="tablist" aria-label="登录身份">
            <button type="button" className={role === "bidder" ? "active" : ""} onClick={() => switchRole("bidder")}>
              用户端
            </button>
            <button type="button" className={role === "admin" ? "active" : ""} onClick={() => switchRole("admin")}>
              管理端
            </button>
          </div>
          <label>
            昵称
            <input value={name} onChange={(event) => setName(event.target.value)} placeholder="输入演示昵称" />
          </label>
          {error && <p className="error-line">{error}</p>}
          <button className="primary-btn" disabled={submitting || !name.trim()}>
            {submitting ? "登录中" : "进入"}
          </button>
        </form>
      </section>
    </main>
  );
}
