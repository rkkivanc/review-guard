"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { ApiClientError, authApi, type AuthTokens, type User } from "@/lib/api";

const REFRESH_KEY = "rg_refresh_token";

type AuthState = {
  user: User | null;
  accessToken: string | null;
  ready: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, name: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refreshProfile: () => Promise<void>;
  updateProfile: (input: { email?: string; name?: string }) => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

function readRefresh(): string | null {
  if (typeof window === "undefined") return null;
  return sessionStorage.getItem(REFRESH_KEY);
}

function writeRefresh(token: string | null) {
  if (typeof window === "undefined") return;
  if (!token) sessionStorage.removeItem(REFRESH_KEY);
  else sessionStorage.setItem(REFRESH_KEY, token);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [ready, setReady] = useState(false);

  const applySession = useCallback((tokens: AuthTokens) => {
    setAccessToken(tokens.access_token);
    setUser(tokens.user);
    writeRefresh(tokens.refresh_token);
  }, []);

  const clearSession = useCallback(() => {
    setAccessToken(null);
    setUser(null);
    writeRefresh(null);
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const refresh = readRefresh();
      if (!refresh) {
        if (!cancelled) setReady(true);
        return;
      }
      try {
        const tokens = await authApi.refresh(refresh);
        if (!cancelled) applySession(tokens);
      } catch {
        if (!cancelled) clearSession();
      } finally {
        if (!cancelled) setReady(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [applySession, clearSession]);

  const login = useCallback(
    async (email: string, password: string) => {
      const tokens = await authApi.login({ email, password });
      applySession(tokens);
    },
    [applySession],
  );

  const register = useCallback(
    async (email: string, name: string, password: string) => {
      const tokens = await authApi.register({ email, name, password });
      applySession(tokens);
    },
    [applySession],
  );

  const logout = useCallback(async () => {
    const refresh = readRefresh();
    try {
      if (refresh) await authApi.logout(refresh);
    } catch (err) {
      if (!(err instanceof ApiClientError)) throw err;
    } finally {
      clearSession();
    }
  }, [clearSession]);

  const refreshProfile = useCallback(async () => {
    if (!accessToken) return;
    const me = await authApi.me(accessToken);
    setUser(me);
  }, [accessToken]);

  const updateProfile = useCallback(
    async (input: { email?: string; name?: string }) => {
      if (!accessToken) throw new Error("Not signed in");
      const me = await authApi.updateMe(accessToken, input);
      setUser(me);
    },
    [accessToken],
  );

  const value = useMemo(
    () => ({
      user,
      accessToken,
      ready,
      login,
      register,
      logout,
      refreshProfile,
      updateProfile,
    }),
    [user, accessToken, ready, login, register, logout, refreshProfile, updateProfile],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
