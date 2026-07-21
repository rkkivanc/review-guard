"use client";

import { GRADE_LEGEND } from "@/lib/grades";

export function GradeLegend({ compact = false }: { compact?: boolean }) {
  return (
    <div className={`grade-legend${compact ? " compact" : ""}`}>
      <h3>What grades mean</h3>
      <p className="muted">
        Grade is derived from the server trust score (0–100), which measures how stable and
        confident the in-browser classifications were — not whether the review is “correct.”
      </p>
      <ul className="grade-list">
        {GRADE_LEGEND.map((g) => (
          <li key={g.grade}>
            <span className={`grade-pill grade-${g.grade.toLowerCase()}`}>{g.grade}</span>
            <span className="mono grade-range">{g.range}</span>
            <span>{g.meaning}</span>
          </li>
        ))}
      </ul>
      <p className="muted">
        <strong>Needs review</strong> is flagged when trust is under 70, or when a dimension had hard
        disagreement across the 3 runs (agreement under 50%).
      </p>
    </div>
  );
}
