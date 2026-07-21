"use client";

import { useEffect, useState } from "react";
import { AppShell, type MasterView } from "@/components/AppShell";
import { AuthView } from "@/components/views/AuthView";
import { DashboardView } from "@/components/views/DashboardView";
import { SimulatorView } from "@/components/views/SimulatorView";
import { useAuth } from "@/lib/auth";

export function App() {
  const { user, ready, logout } = useAuth();
  const [view, setView] = useState<MasterView>("auth");
  const authenticated = Boolean(user);

  useEffect(() => {
    if (!ready) return;
    if (!authenticated) {
      setView("auth");
      return;
    }
    // After login / session restore: leave Auth and open Simulator
    if (view === "auth") {
      setView("simulator");
    }
  }, [ready, authenticated, view]);

  async function handleSignOut() {
    await logout();
    setView("auth");
  }

  if (!ready) {
    return (
      <div className="app-root">
        <main className="shell-main">
          <p className="muted">Loading…</p>
        </main>
      </div>
    );
  }

  return (
    <AppShell
      view={view}
      onViewChange={setView}
      authenticated={authenticated}
      userLabel={user?.email ?? null}
      onSignOut={handleSignOut}
    >
      {!authenticated && <AuthView />}
      {authenticated && view === "simulator" && <SimulatorView />}
      {authenticated && view === "dashboard" && <DashboardView />}
    </AppShell>
  );
}
