import { useState, type FormEvent } from "react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { useAuth } from "../auth";
import { ErrorBox } from "../components";

const DEMO_USERS: [string, string][] = [
  ["prod", "A1 · Оператор производства"],
  ["retail", "A2 · Оператор розницы"],
  ["qc", "A3 · Контролёр качества"],
  ["auditor", "A4 · Аудитор"],
  ["admin", "Администратор"],
];

export default function LoginPage() {
  const { user, loading, login } = useAuth();
  const navigate = useNavigate();
  const loc = useLocation();
  const from = (loc.state as { from?: string } | null)?.from ?? "/";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  if (!loading && user) return <Navigate to={from} replace />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(username, password);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-wrap">
      <div className="login-box">
        <h1 className="login-brand">MarkTrace</h1>
        <p className="login-desc">Учебный макет учёта жизненного цикла кодов маркировки</p>
        <form className="card login-card" onSubmit={submit}>
          <div className="field">
            <label className="field-label" htmlFor="login-username">
              Логин
            </label>
            <input
              id="login-username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
              autoComplete="username"
            />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="login-password">
              Пароль
            </label>
            <input
              id="login-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
            />
          </div>
          <ErrorBox error={error} />
          <button type="submit" className="btn btn-primary btn-block" disabled={busy || !username}>
            {busy ? "Вход…" : "Войти"}
          </button>
        </form>
        <div className="demo-users">
          <div className="demo-users-title">Демо-пользователи, пароль совпадает с логином</div>
          <table className="table">
            <thead>
              <tr>
                <th>Логин</th>
                <th>Роль</th>
              </tr>
            </thead>
            <tbody>
              {DEMO_USERS.map(([u, label]) => (
                <tr key={u}>
                  <td>
                    <button
                      type="button"
                      className="btn-link"
                      title="Подставить логин и пароль"
                      onClick={() => {
                        setUsername(u);
                        setPassword(u);
                      }}
                    >
                      <span className="mono">{u}</span>
                    </button>
                  </td>
                  <td className="muted">{label}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
