"use client";

import { useState, type FormEvent } from "react";
import { GradeFeedbackForm } from "@/components/GradeFeedbackForm";
import { GradeLegend } from "@/components/GradeLegend";
import { ApiClientError, reviewsApi, type ReviewDetail } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import {
  majorityAgreement,
  type ClassificationRun,
} from "@/lib/classify";
import { explainGrade } from "@/lib/grades";

type Subview = "form" | "result";

export function SimulatorView() {
  const { accessToken } = useAuth();

  const [subview, setSubview] = useState<Subview>("form");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [gameName, setGameName] = useState("");
  const [stars, setStars] = useState(8);
  const [reviewText, setReviewText] = useState("");

  const [runs, setRuns] = useState<ClassificationRun[] | null>(null);
  const [latencyMs, setLatencyMs] = useState(0);
  const [saved, setSaved] = useState<ReviewDetail | null>(null);

  async function onClassify(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSaved(null);
    if (!accessToken) {
      setError("Session expired. Sign in again.");
      return;
    }
    setBusy(true);
    try {
      const detail = await reviewsApi.create(accessToken, {
        game_name: gameName.trim(),
        stars,
        review_text: reviewText.trim(),
      });
      const payloads = (detail.runs || []).map((r) => r.payload) as ClassificationRun[];
      setRuns(payloads);
      setLatencyMs(detail.review.latency_ms);
      setSaved(detail);
      setSubview("result");
    } catch (err) {
      setError(formatErr(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <h1>Review Simulator</h1>
      <p className="muted">
        Submit a review — the backend MLC LLM classifies it (3 runs), then trust is scored
        server-side.
      </p>

      <div className="subnav" role="tablist" aria-label="Simulator subviews">
        <button
          type="button"
          className={subview === "form" ? "active" : ""}
          onClick={() => setSubview("form")}
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

      {subview === "form" && (
        <form className="panel" onSubmit={onClassify}>
          <h2>Review form</h2>
          <p className="muted">
            Classification runs in the local <code>mlc-llm</code> Docker service via the API — no
            in-browser model download.
          </p>
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
          <button className="btn" type="submit" disabled={busy}>
            {busy ? "Classifying ×3 + scoring…" : "Classify & save"}
          </button>
        </form>
      )}

      {subview === "result" && runs && (
        <div className="panel">
          <h2>Classification result</h2>
          <p className="muted mono">3 runs via backend MLC · {latencyMs} ms</p>

          {saved ? (
            <div className="review-hero">
              <div className="muted">
                {saved.review.game_name} · {saved.review.stars}/10
              </div>
              <p className="review-body">{saved.review.review_text}</p>
            </div>
          ) : (
            <div className="review-hero">
              <div className="muted">
                {gameName} · {stars}/10
              </div>
              <p className="review-body">{reviewText}</p>
            </div>
          )}

          <h3>Classification (majority labels)</h3>
          <p className="muted">
            Main output: what the model decided across the four dimensions, and how often the 3 runs
            agreed.
          </p>
          <div className="agree-grid">
            {(["consistency", "authenticity", "experience", "usefulness"] as const).map((dim) => {
              const { majority, agreement } = majorityAgreement(runs, dim);
              return (
                <div key={dim} className="agree-card">
                  <div className="agree-dim">{dim}</div>
                  <div className="mono label-lg">{majority}</div>
                  <div className="muted">{Math.round(agreement * 100)}% agreement across runs</div>
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

          {saved ? (
            <GradeFeedbackForm
              reviewId={saved.review.id}
              modelLabels={Object.fromEntries(
                (saved.breakdown || []).map((b) => [b.dimension, b.final_label]),
              )}
              initial={saved.review.feedback}
              onSaved={(review) =>
                setSaved((prev) =>
                  prev
                    ? {
                        ...prev,
                        review: {
                          ...prev.review,
                          feedback: review.feedback,
                          correctness_score: review.correctness_score,
                          correctness_label: review.correctness_label,
                        },
                      }
                    : prev,
                )
              }
            />
          ) : null}

          {saved ? (
            <div className="stats-strip">
              <h3>Trust statistics</h3>
              <p className="muted">
                Secondary signal: how stable/confident the classification was (not review quality).
              </p>
              <div className="stats-row">
                <div>
                  <span className="muted">Trust</span>{" "}
                  <span className="mono">{saved.review.trust_score.toFixed(1)}</span>
                </div>
                <div>
                  <span className="muted">Grade</span>{" "}
                  <span className={`grade-pill grade-${saved.review.grade.toLowerCase()}`}>
                    {saved.review.grade}
                  </span>
                </div>
                <div>
                  <span className="muted">Needs review</span>{" "}
                  <span className={saved.review.needs_review ? "flag-yes" : "flag-no"}>
                    {saved.review.needs_review ? "Yes" : "No"}
                  </span>
                </div>
              </div>
              <p className="muted">{explainGrade(saved.review.grade)}</p>
              <details className="run-details">
                <summary>What do grades mean?</summary>
                <GradeLegend compact />
              </details>
            </div>
          ) : null}

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
