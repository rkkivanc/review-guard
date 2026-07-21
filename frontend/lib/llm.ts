"use client";

import type { MLCEngineInterface } from "@mlc-ai/web-llm";
import {
  buildClassifyPrompt,
  DEFAULT_MODEL_ID,
  parseClassificationJSON,
  type ClassificationRun,
} from "@/lib/classify";

export type LoadProgress = {
  progress: number; // 0–1
  text: string;
};

const READY_KEY = "rg_model_ready";

let cachedEngine: MLCEngineInterface | null = null;
let loadedModelId: string | null = null;
let loadingModelId: string | null = null;
let enginePromise: Promise<MLCEngineInterface> | null = null;

function rememberReady(modelId: string | null) {
  if (typeof window === "undefined") return;
  if (modelId) sessionStorage.setItem(READY_KEY, modelId);
  else sessionStorage.removeItem(READY_KEY);
}

/** Sync: engine is already in memory this page session. */
export function getCachedEngine(): MLCEngineInterface | null {
  return cachedEngine;
}

export function getLoadedModelId(): string | null {
  if (loadedModelId) return loadedModelId;
  if (typeof window === "undefined") return null;
  return sessionStorage.getItem(READY_KEY);
}

export function isModelReady(modelId: string = DEFAULT_MODEL_ID): boolean {
  return cachedEngine !== null && loadedModelId === modelId;
}

export async function loadEngine(
  modelId: string = DEFAULT_MODEL_ID,
  onProgress?: (p: LoadProgress) => void,
): Promise<MLCEngineInterface> {
  if (cachedEngine && loadedModelId === modelId) {
    onProgress?.({ progress: 1, text: "Model already loaded in this session" });
    return cachedEngine;
  }

  if (enginePromise && loadingModelId === modelId) {
    return enginePromise;
  }

  loadingModelId = modelId;
  enginePromise = (async () => {
    const { CreateMLCEngine } = await import("@mlc-ai/web-llm");
    const engine = await CreateMLCEngine(modelId, {
      initProgressCallback: (report) => {
        onProgress?.({
          progress: report.progress ?? 0,
          text: report.text || "Loading model…",
        });
      },
    });
    cachedEngine = engine;
    loadedModelId = modelId;
    rememberReady(modelId);
    onProgress?.({ progress: 1, text: "Model ready" });
    return engine;
  })();

  try {
    return await enginePromise;
  } catch (err) {
    enginePromise = null;
    loadingModelId = null;
    cachedEngine = null;
    loadedModelId = null;
    rememberReady(null);
    throw err;
  }
}

export async function classifyReviewThreeTimes(
  engine: MLCEngineInterface,
  input: { gameName: string; stars: number; reviewText: string },
  temperature = 0.7,
): Promise<{ runs: ClassificationRun[]; latencyMs: number }> {
  const prompt = buildClassifyPrompt(input.gameName, input.stars, input.reviewText);
  const runs: ClassificationRun[] = [];
  const started = performance.now();

  for (let i = 0; i < 3; i++) {
    const reply = await engine.chat.completions.create({
      messages: [{ role: "user", content: prompt }],
      temperature,
      max_tokens: 512,
    });
    const content = reply.choices[0]?.message?.content;
    const text = typeof content === "string" ? content : "";
    runs.push(parseClassificationJSON(text));
  }

  return { runs, latencyMs: Math.round(performance.now() - started) };
}
