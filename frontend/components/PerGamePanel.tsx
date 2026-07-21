"use client";

import { useEffect, useState } from "react";
import { ApiClientError, reviewsApi, type GameAverage } from "@/lib/api";
import { useAuth } from "@/lib/auth";

export function PerGamePanel() {
  const { accessToken } = useAuth();
  const [games, setGames] = useState<string[]>([]);
  const [selected, setSelected] = useState("");
  const [avg, setAvg] = useState<GameAverage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!accessToken) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      try {
        const data = await reviewsApi.games(accessToken);
        if (cancelled) return;
        setGames(data.games);
        setSelected((prev) => prev || data.games[0] || "");
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof ApiClientError ? err.message : "Failed to load games.");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken]);

  useEffect(() => {
    if (!accessToken || !selected) {
      setAvg(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const data = await reviewsApi.gameAverage(accessToken, selected);
        if (!cancelled) {
          setError(null);
          setAvg(data);
        }
      } catch (err) {
        if (!cancelled) {
          setAvg(null);
          setError(err instanceof ApiClientError ? err.message : "Failed to load average.");
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [accessToken, selected]);

  return (
    <div className="panel">
      <h2>Per-game breakdown</h2>
      <p className="muted">
        Raw star mean vs weighted mean (correctness when scored, else stability trust).
      </p>

      {error ? <div className="error-box">{error}</div> : null}

      {loading ? (
        <p className="muted">Loading games…</p>
      ) : games.length === 0 ? (
        <p className="muted">No games yet — classify reviews in the Simulator first.</p>
      ) : (
        <>
          <div className="field">
            <label htmlFor="game-picker">Game</label>
            <select
              id="game-picker"
              value={selected}
              onChange={(e) => {
                setError(null);
                setSelected(e.target.value);
              }}
            >
              {games.map((g) => (
                <option key={g} value={g}>
                  {g}
                </option>
              ))}
            </select>
          </div>

          {avg ? (
            <div className="avg-compare per-game-avg">
              <div className="muted" style={{ gridColumn: "1 / -1" }}>
                {avg.game_name} · {avg.review_count} review{avg.review_count === 1 ? "" : "s"}
              </div>
              <div className="avg-col">
                <span className="avg-label">Raw mean</span>
                <span className="avg-num mono">{avg.raw_avg.toFixed(2)}</span>
                <span className="avg-unit muted">/10</span>
              </div>
              <div className="avg-col">
                <span className="avg-label">Weighted</span>
                <span className="avg-num mono">{avg.weighted_avg.toFixed(2)}</span>
                <span className="avg-unit muted">/10</span>
              </div>
              <p className="avg-note muted">
                {Math.abs(avg.inflation) < 0.05
                  ? "Raw and weighted are nearly the same for this game."
                  : avg.inflation > 0
                    ? `Raw sits ${avg.inflation.toFixed(1)} above weighted.`
                    : `Weighted sits ${Math.abs(avg.inflation).toFixed(1)} above raw.`}
              </p>
            </div>
          ) : (
            <p className="muted">Pick a game to see averages.</p>
          )}
        </>
      )}
    </div>
  );
}
