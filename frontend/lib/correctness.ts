import type { ClassificationFeedback } from "@/lib/api";

const DIMS = ["consistency", "authenticity", "experience", "usefulness"] as const;

/** 0–100 from human Correct/Wrong marks. */
export function correctnessScore(fb: ClassificationFeedback): number {
  const ok = DIMS.filter((d) => fb[d].correct).length;
  return (ok / DIMS.length) * 100;
}

export function correctnessLabel(fb: ClassificationFeedback): string {
  const ok = DIMS.filter((d) => fb[d].correct).length;
  return `${ok}/4`;
}

export function summarizeLabelScores(fb: ClassificationFeedback): string {
  const ok = DIMS.filter((d) => fb[d].correct).length;
  const wrong = DIMS.length - ok;
  if (wrong === 0) return "4/4 correct";
  if (ok === 0) return "0/4 correct";
  return `${ok}/4 correct`;
}
