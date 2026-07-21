"use client";

import { Fragment, useCallback, useEffect, useState } from "react";
import { DashboardOverview } from "@/components/DashboardOverview";
import { GradeFeedbackForm } from "@/components/GradeFeedbackForm";
import { GradeLegend } from "@/components/GradeLegend";
import { PerGamePanel } from "@/components/PerGamePanel";
import {
  ApiClientError,
  reviewsApi,
  type ReviewDetail,
  type ReviewSummary,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { explainGrade } from "@/lib/grades";
import { summarizeLabelScores } from "@/lib/correctness";

type Subview = "overview" | "classifications" | "per-game" | "grades";

export function DashboardView() {
  const { accessToken } = useAuth();
  const [subview, setSubview] = useState<Subview>("overview");
  const [items, setItems] = useState<ReviewSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<ReviewDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const loadList = useCallback(async () => {
    if (!accessToken) return;
    setLoading(true);
    setError(null);
    try {
      const data = await reviewsApi.list(accessToken, { limit: 50, offset: 0 });
      setItems(data.items);
      setTotal(data.total);
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : "Failed to load reviews.");
    } finally {
      setLoading(false);
    }
  }, [accessToken]);

  useEffect(() => {
    void loadList();
  }, [loadList]);

  async function openDetail(id: string) {
    setSubview("classifications");
    if (!accessToken) return;
    if (selectedId === id) {
      setSelectedId(null);
      setDetail(null);
      return;
    }
    setSelectedId(id);
    setDetail(null);
    setDetailLoading(true);
    setError(null);
    try {
      const d = await reviewsApi.get(accessToken, id);
      setDetail(d);
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : "Failed to load classification.");
      setSelectedId(null);
    } finally {
      setDetailLoading(false);
    }
  }

  async function onDelete(id: string) {
    if (!accessToken) return;
    if (!confirm("Delete this review and its classifications?")) return;
    try {
      await reviewsApi.remove(accessToken, id);
      if (selectedId === id) {
        setSelectedId(null);
        setDetail(null);
      }
      await loadList();
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : "Delete failed.");
    }
  }

  return (
    <div>
      <h1>Classifications</h1>
      <p className="muted">
        Score whether labels are correct — that is the primary score. Trust is a secondary stability
        statistic.
      </p>

      <div className="subnav" role="tablist" aria-label="Dashboard subviews">
        <button
          type="button"
          className={subview === "overview" ? "active" : ""}
          onClick={() => setSubview("overview")}
        >
          Overview
        </button>
        <button
          type="button"
          className={subview === "classifications" ? "active" : ""}
          onClick={() => setSubview("classifications")}
        >
          Classifications
        </button>
        <button
          type="button"
          className={subview === "per-game" ? "active" : ""}
          onClick={() => setSubview("per-game")}
        >
          Per-game
        </button>
        <button
          type="button"
          className={subview === "grades" ? "active" : ""}
          onClick={() => setSubview("grades")}
        >
          Grade guide
        </button>
      </div>

      {error ? <div className="error-box">{error}</div> : null}

      {subview === "overview" && (
        <DashboardOverview recent={items} onOpenReview={(id) => void openDetail(id)} />
      )}

      {subview === "per-game" && <PerGamePanel />}

      {subview === "grades" && (
        <div className="panel">
          <GradeLegend />
        </div>
      )}

      {subview === "classifications" && (
        <>
          <div className="panel" style={{ marginBottom: "1rem" }}>
            <div className="dash-toolbar">
              <p className="muted" style={{ margin: 0 }}>
                {loading ? "Loading…" : `${total} review${total === 1 ? "" : "s"}`}
              </p>
              <button
                type="button"
                className="btn secondary"
                onClick={() => void loadList()}
                disabled={loading}
              >
                Refresh
              </button>
            </div>
          </div>

          {!loading && items.length === 0 ? (
            <div className="panel">
              <p className="muted">
                No reviews yet — run one in the Simulator to see classifications here.
              </p>
            </div>
          ) : (
            <div className="review-table-wrap panel">
              <table className="review-table">
                <thead>
                  <tr>
                    <th>Game</th>
                    <th>Review</th>
                    <th>Your rating</th>
                    <th>Correctness</th>
                    <th>Trust</th>
                    <th>Grade</th>
                    <th>Flag</th>
                    <th>When</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((r) => (
                    <Fragment key={r.id}>
                      <tr className={selectedId === r.id ? "selected" : ""}>
                        <td>
                          <button
                            type="button"
                            className="linkish"
                            onClick={() => void openDetail(r.id)}
                          >
                            {r.game_name}
                          </button>
                        </td>
                        <td className="review-snip">{snip(r.review_text)}</td>
                        <td className="mono">{r.stars}/10</td>
                        <td className="mono">
                          {r.feedback ? (
                            <span className="feedback-chip" title="Human label correctness">
                              {r.correctness_label || summarizeLabelScores(r.feedback)}
                              {r.correctness_score != null
                                ? ` · ${r.correctness_score.toFixed(0)}`
                                : ""}
                            </span>
                          ) : (
                            <span className="muted">Unscored</span>
                          )}
                        </td>
                        <td className="mono muted">{r.trust_score.toFixed(1)}</td>
                        <td>
                          <span
                            className={`grade-pill grade-${r.grade.toLowerCase()}`}
                            title={explainGrade(r.grade)}
                          >
                            {r.grade}
                          </span>
                        </td>
                        <td>{r.needs_review ? <span className="flag-yes">Review</span> : "—"}</td>
                        <td className="mono muted">{new Date(r.created_at).toLocaleString()}</td>
                        <td>
                          <button
                            type="button"
                            className="sign-out-link"
                            onClick={() => void onDelete(r.id)}
                          >
                            Delete
                          </button>
                        </td>
                      </tr>
                      {selectedId === r.id ? (
                        <tr className="detail-row">
                          <td colSpan={9}>
                            {detailLoading && <p className="muted">Loading classification…</p>}
                            {detail && detail.review.id === r.id && (
                              <div className="detail-block">
                                <h3>Review</h3>
                                <p className="review-body">{detail.review.review_text}</p>

                                <h3>Classification labels</h3>
                                <div className="agree-grid">
                                  {detail.breakdown.map((b) => (
                                    <div key={b.id} className="agree-card">
                                      <div className="agree-dim">{b.dimension}</div>
                                      <div className="mono label-lg">{b.final_label}</div>
                                      <div className="muted">
                                        {Math.round(b.agreement * 100)}% agree · conf{" "}
                                        {b.avg_confidence.toFixed(2)}
                                      </div>
                                    </div>
                                  ))}
                                </div>

                                <details>
                                  <summary>Raw model runs (3)</summary>
                                  {detail.runs.map((run) => (
                                    <pre key={run.id} className="mono run-pre">
                                      {JSON.stringify(run.payload, null, 2)}
                                    </pre>
                                  ))}
                                </details>

                                <GradeFeedbackForm
                                  reviewId={detail.review.id}
                                  modelLabels={Object.fromEntries(
                                    detail.breakdown.map((b) => [b.dimension, b.final_label]),
                                  )}
                                  initial={detail.review.feedback}
                                  onSaved={(review) => {
                                    setDetail((prev) =>
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
                                    );
                                    setItems((prev) =>
                                      prev.map((it) =>
                                        it.id === detail.review.id
                                          ? {
                                              ...it,
                                              feedback: review.feedback,
                                              correctness_score: review.correctness_score,
                                              correctness_label: review.correctness_label,
                                            }
                                          : it,
                                      ),
                                    );
                                  }}
                                />

                                <div className="stats-strip">
                                  <h3>Trust statistics</h3>
                                  <p className="muted">{explainGrade(detail.review.grade)}</p>
                                  <div className="stats-row">
                                    <div>
                                      <span className="muted">Trust</span>{" "}
                                      <span className="mono">
                                        {detail.review.trust_score.toFixed(1)}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="muted">Grade</span>{" "}
                                      <span
                                        className={`grade-pill grade-${detail.review.grade.toLowerCase()}`}
                                      >
                                        {detail.review.grade}
                                      </span>
                                    </div>
                                    <div>
                                      <span className="muted">Needs review</span>{" "}
                                      {detail.review.needs_review ? (
                                        <span className="flag-yes">Yes</span>
                                      ) : (
                                        "No"
                                      )}
                                    </div>
                                  </div>
                                </div>
                              </div>
                            )}
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function snip(text: string, n = 72): string {
  const t = text.trim().replace(/\s+/g, " ");
  if (t.length <= n) return t;
  return `${t.slice(0, n)}…`;
}
