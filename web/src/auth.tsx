import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, onUnauthorized, type OpType, type Role, type User } from "./api";

// Role permissions mirror SPEC §3/§6; the backend remains the authority (403 AUTH_DENIED).
export function opTypesFor(role: Role | undefined): OpType[] {
  switch (role) {
    case "A1":
      return ["issue", "introduce"];
    case "A2":
      return ["sell", "return", "dispose"];
    case "admin":
      return ["issue", "introduce", "sell", "return", "dispose"];
    default:
      return [];
  }
}

export const canOperate = (role?: Role) => opTypesFor(role).length > 0;
export const canIssue = (role?: Role) => opTypesFor(role).includes("issue");
export const canImport = (role?: Role) => role === "A1" || role === "A3" || role === "admin";
export const canResolve = (role?: Role) => role === "A3" || role === "admin";

interface AuthState {
  user: User | null;
  loading: boolean;
  login: (u: string, p: string) => Promise<void>;
  logout: () => Promise<void>;
}

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    onUnauthorized(() => setUser(null));
    api
      .me()
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setLoading(false));
  }, []);

  const login = useCallback(async (u: string, p: string) => {
    const logged = await api.login(u, p);
    // Prefer the canonical /auth/me shape; fall back to the login body.
    try {
      setUser(await api.me());
    } catch {
      setUser(logged);
    }
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setUser(null);
    }
  }, []);

  return <Ctx.Provider value={{ user, loading, login, logout }}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth outside AuthProvider");
  return v;
}
