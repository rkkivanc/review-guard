"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { ApiClientError, reviewsApi, type Analytics, type ReviewSummary } from "@/lib/api";
import { useAuth } from "@/lib/auth";

type Props = {
  recent: ReviewSummary[];
  onOpenReview: (id: string) => void;
};

export function DashboardOverview({ recent, onOpenReview }: Props) {
  const { accessToken } = useAuth();
  const [analytics, setAnalytics] = useState<Analytics | null>(null);
  const [threshold, setThreshold] = useState(70);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!accessToken) return;
    setLoading(true);
    setError(null);
    try {
      const data = await reviewsApi.analytics(accessToken, threshold);
      setAnalytics(data);
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : "Failed to load analytics.");
    } finally {
      setLoading(false);
    }
  }, [accessToken, threshold]);

  useEffect(() => {
    void load();
  }, [load]);

  const unscored = useMemo(
    () => recent.filter((r) => !r.feedback).slice(0, 8),
    [recent],
  );

  const lowTrust = useMemo(
    () => recent.filter((r) => r.needs_review || r.trust_score < threshold).slice(0, 8),
    [recent, threshold],
  );

  const overallRaw = useMemo(() => {
    if (recent.length === 0) return null;
    const raw = recent.reduce((s, r) => s + r.stars, 0) / recent.length;
    let wSum = 0;
    let tSum = 0;
    for (const r of recent) {
      const w = r.correctness_score != null ? r.correctness_score : r.trust_score;
      wSum += r.stars * w;
      tSum += w;
    }
    const weighted = tSum > 0 ? wSum / tSum : raw;
    return { raw, weighted, inflation: raw - weighted };
  }, [recent]);

  return (
    <div>
      {error ? <div className="error-box">{error}</div> : null}

      <div className="metric-grid">
        <div className="metric-card">
          <div className="muted">Total reviews</div>
          <div className="metric-value mono">{loading ? "…" : (analytics?.total_reviews ?? 0)}</div>
        </div>
        <div className="metric-card metric-primary">
          <div className="muted">Avg correctness</div>
          <div className="metric-value mono">
            {loading
              ? "…"
              : analytics && analytics.feedback_scored_count > 0
                ? analytics.avg_correctness_score.toFixed(0)
                : "—"}
          </div>
          <div className="mono muted" style={{ fontSize: "0.8rem" }}>
            {loading
              ? ""
              : `${analytics?.feedback_scored_count ?? 0} scored · ${
                  (analytics?.total_reviews ?? 0) - (analytics?.feedback_scored_count ?? 0)
                } waiting`}
          </div>
        </div>
        <div className="metric-card">
          <div className="muted">Flagged (stability)</div>
          <div className="metric-value mono">
            {loading ? "…" : (analytics?.flagged_count ?? 0)}
          </div>
        </div>
        <div className="metric-card">
          <div className="muted">Avg stability (trust)</div>
          <div className="metric-value mono">
            {loading ? "…" : (analytics?.avg_trust_score ?? 0).toFixed(0)}
          </div>
        </div>
      </div>

      <div className="panel" style={{ marginTop: "1rem" }}>
        <h3>Star averages</h3>
        <p className="muted">
          Weighted uses your correctness score when scored, otherwise stability trust — so wrong
          classifications pull less weight once you score them.
        </p>
        {overallRaw ? (
          <div className="avg-compare">
            <div className="avg-col">
              <span className="avg-label">Raw mean</span>
              <span className="avg-num mono">{overallRaw.raw.toFixed(1)}</span>
              <span className="avg-unit muted">/10</span>
            </div>
            <div className="avg-col">
              <span className="avg-label">Weighted</span>
              <span className="avg-num mono">{overallRaw.weighted.toFixed(1)}</span>
              <span className="avg-unit muted">/10</span>
            </div>
            <p className="avg-note muted">
              {Math.abs(overallRaw.inflation) < 0.05
                ? "Almost the same — weighting barely moves the mean."
                : overallRaw.inflation > 0
                  ? `Difference ${overallRaw.inflation.toFixed(1)} — raw is higher than weighted.`
                  : `Difference ${Math.abs(overallRaw.inflation).toFixed(1)} — weighted is higher than raw.`}
            </p>
          </div>
        ) : (
          <p className="muted">Classify reviews in the Simulator to unlock this.</p>
        )}
      </div>

      <div className="panel" style={{ marginTop: "1rem" }}>
        <h3>Needs label score</h3>
        {unscored.length === 0 ? (
          <p className="muted">All listed reviews have a correctness score.</p>
        ) : (
          <ul className="activity-list">
            {unscored.map((r) => (
              <li key={r.id}>
                <button type="button" className="linkish" onClick={() => onOpenReview(r.id)}>
                  {r.game_name}
                </button>
                <span className="mono muted"> · {r.stars}/10 · unscored</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="panel" style={{ marginTop: "1rem" }}>
        <h3>Label distribution</h3>
        <p className="muted">How often each final label appeared across your classifications.</p>
        {analytics ? (
          <div className="dist-grid">
            {Object.entries(analytics.dimension_distribution || {}).map(([dim, labels]) => {
              const total = Object.values(labels).reduce((a, b) => a + b, 0) || 1;
              return (
                <div key={dim} className="dist-card">
                  <div className="agree-dim">{dim}</div>
                  {Object.entries(labels).map(([label, count]) => (
                    <div key={label} className="dist-bar-row">
                      <span className="mono dist-label">{label}</span>
                      <div className="dist-track">
                        <div
                          className="dist-fill"
                          style={{ width: `${Math.round((count / total) * 100)}%` }}
                        />
                      </div>
                      <span className="mono muted">{count}</span>
                    </div>
                  ))}
                </div>
              );
            })}
          </div>
        ) : (
          <p className="muted">{loading ? "Loading…" : "No data yet."}</p>
        )}
      </div>

      <div className="panel" style={{ marginTop: "1rem" }}>
        <h3>Recent activity</h3>
        {recent.length === 0 ? (
          <p className="muted">No reviews yet — classify one in the Simulator.</p>
        ) : (
          <ul className="activity-list">
            {recent.slice(0, 8).map((r) => (
              <li key={r.id}>
                <button type="button" className="linkish" onClick={() => onOpenReview(r.id)}>
                  {r.game_name}
                </button>
                <span className="mono muted">
                  {" "}
                  · {r.stars}/10 ·{" "}
                  {r.correctness_label
                    ? `correctness ${r.correctness_label}`
                    : "unscored"}{" "}
                  · stability {r.trust_score.toFixed(0)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <details className="panel" style={{ marginTop: "1rem" }}>
        <summary>Stability threshold preview</summary>
        <p className="muted">
          Preview only — how many reviews would fall under this trust cutoff. Does not change stored
          flags.
        </p>
        <div className="threshold-row">
          <input
            type="range"
            min={0}
            max={100}
            step={1}
            value={threshold}
            onChange={(e) => setThreshold(Number(e.target.value))}
            aria-label="Trust threshold"
          />
          <span className="mono">{threshold}</span>
        </div>
        <p className="mono">
          Would flag:{" "}
          <strong>{loading ? "…" : (analytics?.would_flag_at_threshold ?? 0)}</strong> /{" "}
          {analytics?.total_reviews ?? 0}
        </p>
        {lowTrust.length > 0 ? (
          <table className="review-table" style={{ marginTop: "0.75rem" }}>
            <thead>
              <tr>
                <th>Game</th>
                <th>Stability</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {lowTrust.map((r) => (
                <tr key={r.id}>
                  <td>
                    <button type="button" className="linkish" onClick={() => onOpenReview(r.id)}>
                      {r.game_name}
                    </button>
                  </td>
                  <td className="mono">{r.trust_score.toFixed(1)}</td>
                  <td>{r.needs_review ? <span className="flag-yes">needs review</span> : "low"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : null}
      </details>
    </div>
  );
}
