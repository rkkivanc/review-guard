"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import type { MLCEngineInterface } from "@mlc-ai/web-llm";
import { GradeLegend } from "@/components/GradeLegend";
import { ApiClientError, reviewsApi, type ReviewDetail } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import {
  DEFAULT_MODEL_ID,
  hasWebGPU,
  majorityAgreement,
  MODEL_OPTIONS,
  type ClassificationRun,
} from "@/lib/classify";
import { explainGrade } from "@/lib/grades";
import {
  classifyReviewThreeTimes,
  getCachedEngine,
  getLoadedModelId,
  isModelReady,
  loadEngine,
} from "@/lib/llm";

type Subview = "loader" | "form" | "result";

export function SimulatorView() {
  const { accessToken } = useAuth();
  const webgpu = useMemo(() => hasWebGPU(), []);

  const [subview, setSubview] = useState<Subview>("loader");
  const [modelId, setModelId] = useState(DEFAULT_MODEL_ID);
  const [engine, setEngine] = useState<MLCEngineInterface | null>(null);
  const [loadPct, setLoadPct] = useState(0);
  const [loadText, setLoadText] = useState("");
  const [loading, setLoading] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [gameName, setGameName] = useState("");
  const [stars, setStars] = useState(8);
  const [reviewText, setReviewText] = useState("");

  const [runs, setRuns] = useState<ClassificationRun[] | null>(null);
  const [latencyMs, setLatencyMs] = useState(0);
  const [saved, setSaved] = useState<ReviewDetail | null>(null);

  useEffect(() => {
    const cached = getCachedEngine();
    const remembered = getLoadedModelId();
    const id = remembered || DEFAULT_MODEL_ID;
    if (cached && isModelReady(id)) {
      setEngine(cached);
      setModelId(id);
      setLoadPct(1);
      setLoadText("Model already loaded in this session");
      setSubview((prev) => (prev === "loader" ? "form" : prev));
      return;
    }
    if (!remembered) return;
    let cancelled = false;
    setRestoring(true);
    setLoadText("Restoring model from browser cache…");
    loadEngine(id, (p) => {
      if (!cancelled) {
        setLoadPct(p.progress);
        setLoadText(p.text);
      }
    })
      .then((eng) => {
        if (!cancelled) {
          setEngine(eng);
          setModelId(id);
          setSubview((prev) => (prev === "loader" ? "form" : prev));
        }
      })
      .catch(() => {
        if (!cancelled) {
          setError("Could not restore the model. Press Load Gemma again.");
        }
      })
      .finally(() => {
        if (!cancelled) setRestoring(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function onLoadModel() {
    setError(null);
    if (!webgpu) {
      setError(
        "WebGPU is not available in this browser. You can still open the Dashboard for saved reviews.",
      );
      return;
    }
    setLoading(true);
    setLoadPct(0);
    setLoadText("Starting download…");
    try {
      const eng = await loadEngine(modelId, (p) => {
        setLoadPct(p.progress);
        setLoadText(p.text);
      });
      setEngine(eng);
      setSubview("form");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load model.");
    } finally {
      setLoading(false);
    }
  }

  async function onClassify(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSaved(null);
    if (!engine) {
      setSubview("loader");
      setError("Load the model first.");
      return;
    }
    if (!accessToken) {
      setError("Session expired. Sign in again.");
      return;
    }
    setBusy(true);
    try {
      const result = await classifyReviewThreeTimes(engine, {
        gameName: gameName.trim(),
        stars,
        reviewText: reviewText.trim(),
      });
      setRuns(result.runs);
      setLatencyMs(result.latencyMs);

      const detail = await reviewsApi.create(accessToken, {
        game_name: gameName.trim(),
        stars,
        review_text: reviewText.trim(),
        latency_ms: result.latencyMs,
        runs: result.runs,
      });
      setSaved(detail);
      setSubview("result");
    } catch (err) {
      setError(formatErr(err));
    } finally {
      setBusy(false);
    }
  }

  const modelReady = Boolean(engine);

  return (
    <div>
      <h1>Review Simulator</h1>
      <p className="muted">
        Classify in the browser with Gemma, then let the server score how trustworthy those labels
        are.
      </p>

      <div className="subnav" role="tablist" aria-label="Simulator subviews">
        <button
          type="button"
          className={subview === "loader" ? "active" : ""}
          onClick={() => setSubview("loader")}
        >
          Model loader
        </button>
        <button
          type="button"
          className={subview === "form" ? "active" : ""}
          onClick={() => setSubview("form")}
          disabled={!modelReady}
        >
          Review form
        </button>
        <button
          type="button"
          className={subview === "result" ? "active" : ""}
          onClick={() => setSubview("result")}
          disabled={!runs}
        >
          Classification result
        </button>
      </div>

      {error ? <div className="error-box">{error}</div> : null}

      {subview === "loader" && (
        <div className="panel">
          <h2>Model loader</h2>
          {!webgpu ? (
            <p className="muted">
              WebGPU is required to run Gemma here (Chrome/Edge 113+). Without it you can still browse
              saved reviews on the Dashboard — classification is disabled.
            </p>
          ) : (
            <>
              <div className="field">
                <label htmlFor="model-id">Model</label>
                <select
                  id="model-id"
                  value={modelId}
                  disabled={loading}
                  onChange={(e) => setModelId(e.target.value)}
                >
                  {MODEL_OPTIONS.map((m) => (
                    <option key={m.id} value={m.id}>
                      {m.label}
                    </option>
                  ))}
                </select>
              </div>
              <p className="muted">
                First load downloads ~1.5&nbsp;GB of weights (cached in the browser). Keep this tab
                open.
              </p>
              {loading || loadPct > 0 ? (
                <div className="progress-wrap" aria-live="polite">
                  <div className="progress-track">
                    <div
                      className="progress-fill"
                      style={{ width: `${Math.round(loadPct * 100)}%` }}
                    />
                  </div>
                  <p className="mono muted">
                    {Math.round(loadPct * 100)}% — {loadText || "Working…"}
                  </p>
                </div>
              ) : null}
              {engine ? (
                <div className="model-ready-banner">
                  <strong>Gemma is loaded</strong>
                  <p className="muted" style={{ margin: "0.35rem 0 0" }}>
                    You do not need to download again this session. Use the Review form to classify.
                  </p>
                </div>
              ) : null}
              <button
                className="btn"
                type="button"
                disabled={loading || restoring || !webgpu}
                onClick={onLoadModel}
              >
                {loading || restoring
                  ? restoring
                    ? "Restoring…"
                    : "Loading…"
                  : engine
                    ? "Reload model"
                    : "Load Gemma"}
              </button>
              {!engine ? (
                <p className="muted" style={{ marginTop: "0.75rem" }}>
                  Press Load Gemma once. After it finishes, this page will remember it until you
                  refresh the browser tab.
                </p>
              ) : null}
            </>
          )}
        </div>
      )}

      {subview === "form" && (
        <form className="panel" onSubmit={onClassify}>
          <h2>Review form</h2>
          {!engine ? (
            <p className="muted">Load Gemma on the Model loader tab first.</p>
          ) : (
            <>
              <div className="field">
                <label htmlFor="game-name">Game name</label>
                <input
                  id="game-name"
                  value={gameName}
                  onChange={(e) => setGameName(e.target.value)}
                  required
                  placeholder="Elden Ring"
                />
              </div>
              <div className="field">
                <label htmlFor="stars">Star rating (1–10)</label>
                <input
                  id="stars"
                  type="number"
                  min={1}
                  max={10}
                  value={stars}
                  onChange={(e) => setStars(Number(e.target.value))}
                  required
                />
              </div>
              <div className="field">
                <label htmlFor="review-text">Review text</label>
                <textarea
                  id="review-text"
                  rows={5}
                  value={reviewText}
                  onChange={(e) => setReviewText(e.target.value)}
                  required
                  placeholder="What did you actually play — or is this hype?"
                />
              </div>
              <button className="btn" type="submit" disabled={busy || !webgpu}>
                {busy ? "Classifying ×3 + scoring…" : "Classify & save"}
              </button>
            </>
          )}
        </form>
      )}

      {subview === "result" && runs && (
        <div className="panel">
          <h2>Classification result</h2>
          <p className="muted mono">Latency {latencyMs} ms · 3 runs @ temperature 0.7</p>

          {saved ? (
            <>
              <div className="trust-banner">
                <div>
                  <div className="muted">Server trust score</div>
                  <div className="trust-score mono">{saved.review.trust_score.toFixed(1)}</div>
                </div>
                <div>
                  <div className="muted">Grade</div>
                  <div className="trust-grade">{saved.review.grade}</div>
                </div>
                <div>
                  <div className="muted">Needs review</div>
                  <div className={saved.review.needs_review ? "flag-yes" : "flag-no"}>
                    {saved.review.needs_review ? "Yes" : "No"}
                  </div>
                </div>
              </div>
              <p className="muted">{explainGrade(saved.review.grade)}</p>
            </>
          ) : null}

          <details className="run-details" style={{ marginBottom: "1rem" }}>
            <summary>What do grades mean?</summary>
            <GradeLegend compact />
          </details>

          <h3>Per-dimension agreement</h3>
          <div className="agree-grid">
            {(["consistency", "authenticity", "experience", "usefulness"] as const).map((dim) => {
              const { majority, agreement } = majorityAgreement(runs, dim);
              return (
                <div key={dim} className="agree-card">
                  <div className="agree-dim">{dim}</div>
                  <div className="mono">{majority}</div>
                  <div className="muted">{Math.round(agreement * 100)}% agreement</div>
                </div>
              );
            })}
          </div>

          <h3>Raw runs</h3>
          {runs.map((run, i) => (
            <details key={i} className="run-details">
              <summary>Run {i + 1}</summary>
              <pre className="mono run-pre">{JSON.stringify(run, null, 2)}</pre>
            </details>
          ))}

          <button className="btn secondary" type="button" onClick={() => setSubview("form")}>
            Classify another
          </button>
        </div>
      )}
    </div>
  );
}

function formatErr(err: unknown): string {
  if (err instanceof ApiClientError) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}
