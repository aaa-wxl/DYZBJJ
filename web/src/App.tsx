import { useEffect, useMemo, useState } from "react";
import type { LoginSession, Role } from "./api";
import { AdminApp } from "./AdminApp";
import { LoginPage } from "./LoginPage";
import { MobileApp } from "./MobileApp";
import { clearSession, loadSession, saveSession } from "./session";

export function App() {
  const [session, setSession] = useState<LoginSession | null>(() => loadSession());
  const [path, setPath] = useState(() => window.location.pathname);

  useEffect(() => {
    const syncPath = () => setPath(window.location.pathname);
    window.addEventListener("popstate", syncPath);
    return () => window.removeEventListener("popstate", syncPath);
  }, []);

  const targetRole = useMemo<Role>(() => (path.startsWith("/admin") ? "admin" : "bidder"), [path]);
  const roleMismatch = session && session.user.role !== targetRole && path !== "/login";

  function navigate(nextPath: string) {
    window.history.pushState({}, "", nextPath);
    setPath(nextPath);
  }

  function handleLogin(nextSession: LoginSession) {
    saveSession(nextSession);
    setSession(nextSession);
    navigate(nextSession.user.role === "admin" ? "/admin" : "/m");
  }

  function handleLogout() {
    clearSession();
    setSession(null);
    navigate("/login");
  }

  if (!session || roleMismatch || path === "/login") {
    return <LoginPage defaultRole={targetRole} onLogin={handleLogin} />;
  }

  if (path.startsWith("/admin")) {
    return <AdminApp session={session} onLogout={handleLogout} />;
  }

  return <MobileApp session={session} onLogout={handleLogout} />;
}
