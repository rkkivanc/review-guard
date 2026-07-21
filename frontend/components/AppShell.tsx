"use client";

export type MasterView = "auth" | "simulator" | "dashboard";

type AppShellProps = {
  view: MasterView;
  onViewChange: (view: MasterView) => void;
  authenticated: boolean;
  userLabel?: string | null;
  onSignOut?: () => void;
  children: React.ReactNode;
};

export function AppShell({
  view,
  onViewChange,
  authenticated,
  userLabel,
  onSignOut,
  children,
}: AppShellProps) {
  return (
    <div className="app-root">
      <header className="shell-header">
        <div className="brand" aria-label="ReviewGuard">
          <span className="brand-mark">ReviewGuard</span>
        </div>

        {authenticated ? (
          <nav className="nav" aria-label="Master views">
            <button
              type="button"
              className={`nav-btn${view === "simulator" ? " active" : ""}`}
              onClick={() => onViewChange("simulator")}
            >
              Simulator
            </button>
            <button
              type="button"
              className={`nav-btn${view === "dashboard" ? " active" : ""}`}
              onClick={() => onViewChange("dashboard")}
            >
              Dashboard
            </button>
          </nav>
        ) : (
          <nav className="nav" aria-label="Master views">
            <button type="button" className="nav-btn active">
              Auth
            </button>
          </nav>
        )}

        {authenticated ? (
          <div className="header-account">
            {userLabel ? <span className="user-chip">{userLabel}</span> : null}
            <button type="button" className="sign-out-link" onClick={onSignOut}>
              Sign out
            </button>
          </div>
        ) : null}
      </header>
      <main className="shell-main">{children}</main>
    </div>
  );
}
