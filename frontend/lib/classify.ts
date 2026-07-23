export const DEFAULT_MODEL_ID = "gemma-2-2b-it-q4f16_1-MLC";

export type LabelResult = {
  label: string;
  confidence: number;
  reason: string;
};

export type ClassificationRun = {
  consistency: LabelResult;
  authenticity: LabelResult;
  experience: LabelResult;
  usefulness: LabelResult;
};

export function majorityAgreement(
  runs: ClassificationRun[],
  dim: keyof ClassificationRun,
): { majority: string; agreement: number } {
  const labels = runs.map((r) => r[dim].label);
  const counts = new Map<string, number>();
  for (const l of labels) counts.set(l, (counts.get(l) || 0) + 1);
  let majority = labels[0] || "";
  let best = 0;
  for (const [l, c] of counts) {
    if (c > best) {
      best = c;
      majority = l;
    }
  }
  return { majority, agreement: labels.length ? best / labels.length : 0 };
}
