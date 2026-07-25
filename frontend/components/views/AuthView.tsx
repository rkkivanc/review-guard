"use client";

import { useState, type FormEvent } from "react";
import { ApiClientError } from "@/lib/api";
import { useAuth } from "@/lib/auth";

type AuthSubview = "login" | "register";

export function AuthView() {
  const { login, register, ready } = useAuth();
  const [subview, setSubview] = useState<AuthSubview>("login");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onLogin(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    const fd = new FormData(e.currentTarget);
    try {
      await login(String(fd.get("email") || ""), String(fd.get("password") || ""));
    } catch (err) {
      setError(formatErr(err));
    } finally {
      setBusy(false);
    }
  }

  async function onRegister(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    const fd = new FormData(e.currentTarget);
    try {
      await register(
        String(fd.get("email") || ""),
        String(fd.get("name") || ""),
        String(fd.get("password") || ""),
      );
    } catch (err) {
      setError(formatErr(err));
    } finally {
      setBusy(false);
    }
  }

  if (!ready) {
    return (
      <div className="panel">
        <p className="muted">Checking session…</p>
      </div>
    );
  }

  return (
    <div>
      <h1>Sign in</h1>
      <p className="muted">
        Create an account or sign in to open Simulator, Dashboard, and DeepKwiki. Register with{" "}
        <code>admin@reviewguard.local</code> for the Admin LLM panel.
      </p>

      <div className="subnav" role="tablist" aria-label="Auth subviews">
        <button
          type="button"
          className={subview === "login" ? "active" : ""}
          onClick={() => setSubview("login")}
        >
          Login
        </button>
        <button
          type="button"
          className={subview === "register" ? "active" : ""}
          onClick={() => setSubview("register")}
        >
          Register
        </button>
      </div>

      {error ? <div className="error-box">{error}</div> : null}

      {subview === "login" && (
        <form className="panel" onSubmit={onLogin}>
          <div className="field">
            <label htmlFor="login-email">Email</label>
            <input id="login-email" name="email" type="email" required autoComplete="email" />
          </div>
          <div className="field">
            <label htmlFor="login-password">Password</label>
            <input
              id="login-password"
              name="password"
              type="password"
              required
              minLength={8}
              autoComplete="current-password"
            />
          </div>
          <button className="btn" type="submit" disabled={busy}>
            {busy ? "Signing in…" : "Sign in"}
          </button>
        </form>
      )}

      {subview === "register" && (
        <form className="panel" onSubmit={onRegister}>
          <div className="field">
            <label htmlFor="reg-name">Name</label>
            <input id="reg-name" name="name" required autoComplete="name" />
          </div>
          <div className="field">
            <label htmlFor="reg-email">Email</label>
            <input id="reg-email" name="email" type="email" required autoComplete="email" />
          </div>
          <div className="field">
            <label htmlFor="reg-password">Password</label>
            <input
              id="reg-password"
              name="password"
              type="password"
              required
              minLength={8}
              autoComplete="new-password"
            />
          </div>
          <button className="btn" type="submit" disabled={busy}>
            {busy ? "Creating…" : "Create account"}
          </button>
        </form>
      )}
    </div>
  );
}

function formatErr(err: unknown): string {
  if (err instanceof ApiClientError) {
    return `${err.message} Try again, or check that the API is running.`;
  }
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}
