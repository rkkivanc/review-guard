"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ApiClientError,
  authApi,
  bindAuthSession,
  readRefreshToken,
  refreshAccessToken,
  setMemoryAccessToken,
  writeRefreshToken,
  type AuthTokens,
  type User,
} from "@/lib/api";

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

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [accessToken, setAccessToken] = useState<string | null>(null);
  const [ready, setReady] = useState(false);
  const [expiresAt, setExpiresAt] = useState<number | null>(null);
  const applyingRef = useRef(false);

  const clearSession = useCallback(() => {
    setAccessToken(null);
    setUser(null);
    setExpiresAt(null);
    setMemoryAccessToken(null);
    writeRefreshToken(null);
  }, []);

  const applySession = useCallback((tokens: AuthTokens) => {
    applyingRef.current = true;
    setAccessToken(tokens.access_token);
    setUser(tokens.user);
    setMemoryAccessToken(tokens.access_token);
    writeRefreshToken(tokens.refresh_token);
    const ttlSec = Math.max(30, Number(tokens.expires_in) || 900);
    setExpiresAt(Date.now() + ttlSec * 1000);
    applyingRef.current = false;
  }, []);

  useEffect(() => {
    bindAuthSession({
      onTokens: (tokens) => {
        if (!applyingRef.current) applySession(tokens);
      },
      onCleared: () => {
        if (!applyingRef.current) clearSession();
      },
    });
    return () => bindAuthSession(null);
  }, [applySession, clearSession]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const refresh = readRefreshToken();
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

  // Proactively rotate access JWT ~60s before expiry.
  useEffect(() => {
    if (!accessToken || !expiresAt) return;
    const delay = Math.max(5_000, expiresAt - Date.now() - 60_000);
    const timer = window.setTimeout(() => {
      void refreshAccessToken();
    }, delay);
    return () => window.clearTimeout(timer);
  }, [accessToken, expiresAt]);

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
    const refresh = readRefreshToken();
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
