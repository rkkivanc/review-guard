/** Trust grade bands — server uses the same thresholds. */
export const GRADE_LEGEND = [
  {
    grade: "A",
    range: "90–100",
    meaning: "High trust. The 3 model runs mostly agreed and reported strong confidence.",
  },
  {
    grade: "B",
    range: "80–89",
    meaning: "Good trust. Labels are fairly stable across runs, with solid confidence.",
  },
  {
    grade: "C",
    range: "70–79",
    meaning: "Moderate trust. Useful, but some disagreement or weaker confidence.",
  },
  {
    grade: "D",
    range: "60–69",
    meaning: "Low trust. Unstable labels or weak confidence — treat carefully.",
  },
  {
    grade: "F",
    range: "0–59",
    meaning: "Very low trust. Runs conflicted or confidence was poor; usually needs review.",
  },
] as const;

export function explainGrade(grade: string): string {
  const hit = GRADE_LEGEND.find((g) => g.grade === grade.toUpperCase());
  return hit ? `${hit.grade} (${hit.range}): ${hit.meaning}` : "Unknown grade.";
}
