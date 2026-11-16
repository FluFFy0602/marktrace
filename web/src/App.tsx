import type { ReactNode } from "react";
import { Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { canOperate, useAuth } from "./auth";
import { Loading } from "./components";
import { ROLE_LABEL } from "./labels";
import CodePage from "./pages/CodePage";
import CodesPage from "./pages/CodesPage";
import ImportDetailPage from "./pages/ImportDetailPage";
import ImportPage from "./pages/ImportPage";
import KpiPage from "./pages/KpiPage";
import LoginPage from "./pages/LoginPage";
import OperationPage from "./pages/OperationPage";
import VerifyPage from "./pages/VerifyPage";
import ViolationsPage from "./pages/ViolationsPage";

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading } = useAuth();
  const loc = useLocation();
  if (loading) return <Loading />;
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />;
  return <>{children}</>;
}

function Shell({ children }: { children: ReactNode }) {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const showOp = canOperate(user?.role);
  const name = user ? user.display_name || user.username : "";
  const roleLabel = user ? ROLE_LABEL[user.role] ?? user.role : "";
  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar-inner">
          <Link className="brand" to="/">
            MarkTrace
          </Link>
          <nav className="nav" aria-label="Разделы">
            {showOp && <NavLink to="/operation">Операция</NavLink>}
            <NavLink to="/codes" end={false}>
              Коды
            </NavLink>
            <NavLink to="/violations">Расхождения</NavLink>
            <NavLink to="/imports">Импорт</NavLink>
            <NavLink to="/kpi">Сводка KPI</NavLink>
            <NavLink to="/verify">Проверка кода</NavLink>
          </nav>
          {user && (
            <div className="userbox">
              <span className="username" title={user.username}>
                {name}
              </span>
              <span className="userrole">{roleLabel !== name ? roleLabel : user.username}</span>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={async () => {
                  await logout();
                  navigate("/login");
                }}
              >
                Выйти
              </button>
            </div>
          )}
        </div>
      </header>
      <main className="content">{children}</main>
    </div>
  );
}

function Home() {
  const { user } = useAuth();
  return <Navigate to={canOperate(user?.role) ? "/operation" : "/codes"} replace />;
}

export default function App() {
  const { user } = useAuth();
  const guarded = (el: ReactNode) => (
    <RequireAuth>
      <Shell>{el}</Shell>
    </RequireAuth>
  );
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={guarded(<Home />)} />
      <Route
        path="/operation"
        element={guarded(canOperate(user?.role) ? <OperationPage /> : <Navigate to="/codes" replace />)}
      />
      <Route path="/codes" element={guarded(<CodesPage />)} />
      <Route path="/codes/:code" element={guarded(<CodePage />)} />
      <Route path="/violations" element={guarded(<ViolationsPage />)} />
      <Route path="/imports" element={guarded(<ImportPage />)} />
      <Route path="/imports/:id" element={guarded(<ImportDetailPage />)} />
      <Route path="/kpi" element={guarded(<KpiPage />)} />
      <Route path="/verify" element={guarded(<VerifyPage />)} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
