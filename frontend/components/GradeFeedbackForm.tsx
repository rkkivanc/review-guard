"use client";

import { useMemo, useState, type FormEvent } from "react";
import {
  ApiClientError,
  reviewsApi,
  type ClassificationFeedback,
  type DimJudgment,
  type ReviewSummary,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { correctnessLabel, correctnessScore, summarizeLabelScores } from "@/lib/correctness";

const LABEL_OPTIONS: Record<string, string[]> = {
  consistency: ["aligned", "mismatched"],
  authenticity: ["genuine", "suspicious", "bot"],
  experience: ["experience_based", "speculative"],
  usefulness: ["useful", "neutral", "empty"],
};

const DIMS = ["consistency", "authenticity", "experience", "usefulness"] as const;

type Props = {
  reviewId: string;
  /** Model majority labels keyed by dimension */
  modelLabels: Record<string, string>;
  initial?: ClassificationFeedback | null;
  onSaved?: (review: ReviewSummary) => void;
};

type Draft = Record<(typeof DIMS)[number], { correct: boolean | null; correctLabel: string }>;

function draftFromInitial(
  modelLabels: Record<string, string>,
  initial?: ClassificationFeedback | null,
): Draft {
  const d = {} as Draft;
  for (const dim of DIMS) {
    const prev = initial?.[dim];
    d[dim] = {
      correct: prev ? prev.correct : null,
      correctLabel: prev?.correct_label || "",
    };
  }
  return d;
}

export function GradeFeedbackForm({ reviewId, modelLabels, initial, onSaved }: Props) {
  const { accessToken } = useAuth();
  const [draft, setDraft] = useState<Draft>(() => draftFromInitial(modelLabels, initial));
  const [note, setNote] = useState(initial?.note || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState<ClassificationFeedback | null>(initial ?? null);
  const [savedScore, setSavedScore] = useState<number | null>(
    initial ? correctnessScore(initial) : null,
  );

  const complete = useMemo(
    () => DIMS.every((dim) => draft[dim].correct !== null && (draft[dim].correct || draft[dim].correctLabel)),
    [draft],
  );

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    if (!accessToken || !complete) return;
    setBusy(true);
    setError(null);
    try {
      const body = {
        note: note || undefined,
        consistency: toJudgment("consistency"),
        authenticity: toJudgment("authenticity"),
        experience: toJudgment("experience"),
        usefulness: toJudgment("usefulness"),
      };
      const review = await reviewsApi.feedback(accessToken, reviewId, body);
      const fb = review.feedback;
      if (fb) {
        setSaved(fb);
        setSavedScore(
          review.correctness_score != null ? review.correctness_score : correctnessScore(fb),
        );
        onSaved?.(review);
      }
    } catch (err) {
      setError(err instanceof ApiClientError ? err.message : "Could not save feedback.");
    } finally {
      setBusy(false);
    }

    function toJudgment(dim: (typeof DIMS)[number]): DimJudgment {
      const row = draft[dim];
      const modelLabel = modelLabels[dim] || "";
      if (row.correct) {
        return { correct: true, model_label: modelLabel };
      }
      return {
        correct: false,
        model_label: modelLabel,
        correct_label: row.correctLabel,
      };
    }
  }

  return (
    <form className="feedback-panel" onSubmit={onSubmit}>
      <h3>Score classification correctness</h3>
      <p className="muted">
        Primary score: mark whether each dimension label is right. Corrections also teach later
        runs. Trust stats stay separate.
      </p>

      {error ? <div className="error-box">{error}</div> : null}

      {saved && savedScore != null ? (
        <div className="correctness-hero">
          <div className="muted">Correctness score</div>
          <div className="correctness-hero-value mono">
            {savedScore.toFixed(0)}
            <span className="correctness-hero-max">/100</span>
          </div>
          <div className="mono muted">
            {correctnessLabel(saved)} · {summarizeLabelScores(saved)}
          </div>
        </div>
      ) : null}

      <div className="dim-score-grid">
        {DIMS.map((dim) => {
          const modelLabel = modelLabels[dim] || "—";
          const row = draft[dim];
          return (
            <div key={dim} className="dim-score-card">
              <div className="agree-dim">{dim}</div>
              <div className="mono">
                Model: <strong>{modelLabel}</strong>
              </div>
              <div className="dim-score-actions">
                <button
                  type="button"
                  className={`chip-btn${row.correct === true ? " ok" : ""}`}
                  onClick={() =>
                    setDraft((prev) => ({
                      ...prev,
                      [dim]: { correct: true, correctLabel: "" },
                    }))
                  }
                >
                  Correct
                </button>
                <button
                  type="button"
                  className={`chip-btn${row.correct === false ? " bad" : ""}`}
                  onClick={() =>
                    setDraft((prev) => ({
                      ...prev,
                      [dim]: {
                        correct: false,
                        correctLabel: prev[dim].correctLabel || LABEL_OPTIONS[dim][0],
                      },
                    }))
                  }
                >
                  Wrong
                </button>
              </div>
              {row.correct === false ? (
                <div className="field" style={{ marginBottom: 0, marginTop: "0.5rem" }}>
                  <label htmlFor={`fix-${reviewId}-${dim}`}>Correct label</label>
                  <select
                    id={`fix-${reviewId}-${dim}`}
                    value={row.correctLabel}
                    onChange={(e) =>
                      setDraft((prev) => ({
                        ...prev,
                        [dim]: { ...prev[dim], correctLabel: e.target.value },
                      }))
                    }
                  >
                    {LABEL_OPTIONS[dim].map((opt) => (
                      <option key={opt} value={opt}>
                        {opt}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}
            </div>
          );
        })}
      </div>

      <div className="field">
        <label htmlFor={`note-${reviewId}`}>Note (optional)</label>
        <textarea
          id={`note-${reviewId}`}
          rows={2}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="e.g. Sounds like hype, not real play"
        />
      </div>

      <button className="btn secondary" type="submit" disabled={busy || !complete}>
        {busy ? "Saving…" : saved ? "Update correctness score" : "Save correctness score"}
      </button>
    </form>
  );
}
