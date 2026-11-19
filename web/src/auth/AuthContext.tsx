import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api, onUnauthorized, setCsrfToken } from "../api/client";
import type { Me, Role } from "../api/types";

const RANK: Record<Role, number> = { readonly: 1, operator: 2, admin: 3 };

interface AuthState {
  me: Me | null;
  loading: boolean;
  /** Verdadero si la sesión terminó porque el administrador la cerró, y no por caducar. */
  loggedOut: boolean;
  /** Vuelve a consultar la sesión en el servidor. */
  refresh(): Promise<void>;
  /** Fija la sesión tras un inicio de sesión correcto. */
  signedIn(me: Me): void;
  logout(): Promise<void>;
  /** Informa de si el administrador alcanza el rol mínimo. */
  can(min: Role): boolean;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);
  const [loggedOut, setLoggedOut] = useState(false);

  const apply = useCallback((m: Me | null) => {
    setCsrfToken(m?.csrf_token ?? null);
    setMe(m);
    if (m) setLoggedOut(false);
  }, []);

  const refresh = useCallback(async () => {
    try {
      apply(await api<Me>("GET", "/auth/me"));
    } catch {
      apply(null);
    } finally {
      setLoading(false);
    }
  }, [apply]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Cualquier 401 de la API significa que la sesión ha caducado o se ha cerrado.
  useEffect(() => {
    onUnauthorized(() => {
      apply(null);
      queryClient.clear();
    });
    return () => onUnauthorized(null);
  }, [apply, queryClient]);

  const logout = useCallback(async () => {
    try {
      await api("POST", "/auth/logout");
    } catch {
      // Aunque falle, la sesión local se cierra igualmente.
    }
    setLoggedOut(true);
    apply(null);
    queryClient.clear();
  }, [apply, queryClient]);

  const value = useMemo<AuthState>(
    () => ({
      me,
      loading,
      loggedOut,
      refresh,
      signedIn: apply,
      logout,
      can: (min) => !!me && RANK[me.role] >= RANK[min],
    }),
    [me, loading, loggedOut, refresh, apply, logout],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth fuera de AuthProvider");
  return ctx;
}
