export type ApiError = {
  code: string;
  message: string;
};

export type Envelope<T> = {
  success: boolean;
  data: T | null;
  error: ApiError | null;
};

export type User = {
  id: string;
  email: string;
  name: string;
  role?: "user" | "admin" | string;
  created_at: string;
};

export type AdapterMeta = {
  id: string;
  name: string;
  path: string;
  description?: string;
  active: boolean;
};

export type LLMRuntimeConfig = {
  system_prompt: string;
  max_tokens: number;
  temperature: number;
  top_p: number;
  active_adapter: string;
};

export type QueryLogEntry = {
  id: string;
  user_id: string;
  tool: string;
  query: string;
  adapter_id?: string;
  model_id?: string;
  latency_ms: number;
  ok: boolean;
  error?: string;
  created_at: string;
};

export type AuthTokens = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: User;
};

const CLOUD_API_URL = "https://reviewguard-api.onrender.com";

/** Ensure scheme so fetch never treats the host as a relative path. */
function normalizeApiUrl(raw: string | undefined | null): string {
  let u = (raw || "").trim().replace(/\/$/, "");
  if (!u) return "";
  // Host-only values (e.g. reviewguard-api.onrender.com) become relative without this.
  if (!/^https?:\/\//i.test(u)) {
    u = `https://${u.replace(/^\/+/, "")}`;
  }
  // Repair https:/host typos
  if (u.startsWith("https:/") && !u.startsWith("https://")) {
    u = `https://${u.slice("https:/".length)}`;
  }
  if (u.startsWith("http:/") && !u.startsWith("http://")) {
    u = `http://${u.slice("http:/".length)}`;
  }
  return u.replace(/\/$/, "");
}

const BAKED_API_URL = normalizeApiUrl(process.env.NEXT_PUBLIC_API_URL);

/** Runtime-safe API base (never return a scheme-less URL). */
export function getApiUrl() {
  if (typeof window !== "undefined") {
    const host = window.location.hostname;
    // Always use the known absolute Render API on Vercel — ignore bad/scheme-less env bakes.
    if (host.endsWith("vercel.app") || host.endsWith("vercel.sh")) {
      return CLOUD_API_URL;
    }
  }
  return BAKED_API_URL || "http://localhost:8080";
}

export class ApiClientError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiClientError";
    this.status = status;
    this.code = code;
  }
}

export const REFRESH_TOKEN_KEY = "rg_refresh_token";

type SessionHooks = {
  onTokens: (tokens: AuthTokens) => void;
  onCleared: () => void;
};

let sessionHooks: SessionHooks | null = null;
let memoryAccessToken: string | null = null;
let refreshInFlight: Promise<string | null> | null = null;

/** AuthProvider registers here so API/MCP can rotate tokens without prop drilling. */
export function bindAuthSession(hooks: SessionHooks | null) {
  sessionHooks = hooks;
}

export function setMemoryAccessToken(token: string | null) {
  memoryAccessToken = token;
}

export function readRefreshToken(): string | null {
  if (typeof window === "undefined") return null;
  return sessionStorage.getItem(REFRESH_TOKEN_KEY);
}

export function writeRefreshToken(token: string | null) {
  if (typeof window === "undefined") return;
  if (!token) sessionStorage.removeItem(REFRESH_TOKEN_KEY);
  else sessionStorage.setItem(REFRESH_TOKEN_KEY, token);
}

function isAuthExchangePath(path: string) {
  return (
    path === "/auth/login" ||
    path === "/auth/register" ||
    path === "/auth/refresh" ||
    path === "/auth/logout"
  );
}

/** Single-flight refresh; returns a fresh access token or null. */
export async function refreshAccessToken(): Promise<string | null> {
  if (refreshInFlight) return refreshInFlight;
  refreshInFlight = (async () => {
    const refresh = readRefreshToken();
    if (!refresh) {
      memoryAccessToken = null;
      sessionHooks?.onCleared();
      return null;
    }
    try {
      const tokens = await apiRequest<AuthTokens>("/auth/refresh", {
        method: "POST",
        body: { refresh_token: refresh },
        anonymous: true,
        skipAuthRetry: true,
      });
      memoryAccessToken = tokens.access_token;
      writeRefreshToken(tokens.refresh_token);
      sessionHooks?.onTokens(tokens);
      return tokens.access_token;
    } catch {
      memoryAccessToken = null;
      writeRefreshToken(null);
      sessionHooks?.onCleared();
      return null;
    } finally {
      refreshInFlight = null;
    }
  })();
  return refreshInFlight;
}

/** Prefer current token; if missing, refresh from sessionStorage. */
export async function ensureAccessToken(
  preferred?: string | null,
): Promise<string | null> {
  if (preferred) return preferred;
  if (memoryAccessToken) return memoryAccessToken;
  return refreshAccessToken();
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  accessToken?: string | null;
  signal?: AbortSignal;
  /** Do not attach Authorization (login/register/refresh/logout). */
  anonymous?: boolean;
  /** Internal: do not attempt 401 → refresh → retry (avoids loops). */
  skipAuthRetry?: boolean;
};

export async function apiRequest<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  return apiRequestOnce<T>(path, options, false);
}

async function apiRequestOnce<T>(
  path: string,
  options: RequestOptions,
  retried: boolean,
): Promise<T> {
  const apiUrl = getApiUrl();
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  const bearer = options.anonymous
    ? null
    : (options.accessToken ?? memoryAccessToken);
  if (bearer) {
    headers.Authorization = `Bearer ${bearer}`;
  }

  let res: Response;
  try {
    res = await fetch(`${apiUrl}${path}`, {
      method: options.method || "GET",
      headers,
      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
      signal: options.signal,
    });
  } catch {
    throw new ApiClientError(
      0,
      "network_error",
      `Failed to reach API at ${apiUrl}. Is NEXT_PUBLIC_API_URL set correctly?`,
    );
  }

  const text = await res.text();
  let envelope: Envelope<T>;
  try {
    envelope = JSON.parse(text) as Envelope<T>;
  } catch {
    const snippet = text.replace(/\s+/g, " ").trim().slice(0, 80);
    throw new ApiClientError(
      res.status,
      "bad_response",
      `Server returned a non-JSON response from ${apiUrl}${path}` +
        (snippet ? ` (${snippet})` : "") +
        ". Try again, or check that the API is running.",
    );
  }

  if (!envelope.success || envelope.data === null) {
    const canRetry =
      !retried &&
      !options.skipAuthRetry &&
      !isAuthExchangePath(path) &&
      res.status === 401 &&
      Boolean(bearer || readRefreshToken());
    if (canRetry) {
      const next = await refreshAccessToken();
      if (next) {
        return apiRequestOnce<T>(
          path,
          { ...options, accessToken: next },
          true,
        );
      }
    }
    throw new ApiClientError(
      res.status,
      envelope.error?.code || "request_failed",
      envelope.error?.message || "Request failed.",
    );
  }

  return envelope.data;
}

export const authApi = {
  register(body: { email: string; name: string; password: string }) {
    return apiRequest<AuthTokens>("/auth/register", {
      method: "POST",
      body,
      anonymous: true,
      skipAuthRetry: true,
    });
  },
  login(body: { email: string; password: string }) {
    return apiRequest<AuthTokens>("/auth/login", {
      method: "POST",
      body,
      anonymous: true,
      skipAuthRetry: true,
    });
  },
  refresh(refresh_token: string) {
    return apiRequest<AuthTokens>("/auth/refresh", {
      method: "POST",
      body: { refresh_token },
      anonymous: true,
      skipAuthRetry: true,
    });
  },
  logout(refresh_token: string) {
    return apiRequest<{ logged_out: boolean }>("/auth/logout", {
      method: "POST",
      body: { refresh_token },
      anonymous: true,
      skipAuthRetry: true,
    });
  },
  me(accessToken: string) {
    return apiRequest<User>("/auth/me", { accessToken });
  },
  updateMe(accessToken: string, body: { email?: string; name?: string }) {
    return apiRequest<User>("/auth/me", {
      method: "PATCH",
      accessToken,
      body,
    });
  },
};

export type ClassificationRunPayload = {
  consistency: { label: string; confidence: number; reason: string };
  authenticity: { label: string; confidence: number; reason: string };
  experience: { label: string; confidence: number; reason: string };
  usefulness: { label: string; confidence: number; reason: string };
};

export type DimJudgment = {
  correct: boolean;
  model_label: string;
  correct_label?: string;
};

export type ClassificationFeedback = {
  consistency: DimJudgment;
  authenticity: DimJudgment;
  experience: DimJudgment;
  usefulness: DimJudgment;
  note?: string;
  created_at: string;
};

export type ReviewSummary = {
  id: string;
  user_id: string;
  game_name: string;
  stars: number;
  review_text: string;
  trust_score: number;
  grade: string;
  needs_review: boolean;
  latency_ms: number;
  created_at: string;
  feedback?: ClassificationFeedback | null;
  correctness_score?: number | null;
  correctness_label?: string;
};

export type ReviewListResponse = {
  items: ReviewSummary[];
  total: number;
  limit: number;
  offset: number;
};

export type FeedbackHint = {
  game_name: string;
  stars: number;
  corrections: Array<{
    dimension: string;
    model_label: string;
    correct: boolean;
    correct_label?: string;
  }>;
  note?: string;
};

export type ReviewDetail = {
  review: {
    id: string;
    user_id: string;
    game_name: string;
    stars: number;
    review_text: string;
    trust_score: number;
    grade: string;
    needs_review: boolean;
    latency_ms: number;
    created_at: string;
    feedback?: ClassificationFeedback | null;
    correctness_score?: number | null;
    correctness_label?: string;
  };
  runs: Array<{ id: string; review_id: string; run_index: number; payload: ClassificationRunPayload }>;
  breakdown: Array<{
    id: string;
    review_id: string;
    dimension: string;
    final_label: string;
    agreement: number;
    avg_confidence: number;
    dim_score: number;
  }>;
  composite: number;
  penalty: number;
};

export const reviewsApi = {
  create(
    accessToken: string,
    body: {
      game_name: string;
      stars: number;
      review_text: string;
    },
  ) {
    return apiRequest<ReviewDetail>("/reviews", {
      method: "POST",
      accessToken,
      body,
    });
  },
  list(accessToken: string, query?: { limit?: number; offset?: number; needs_review?: boolean }) {
    const params = new URLSearchParams();
    if (query?.limit != null) params.set("limit", String(query.limit));
    if (query?.offset != null) params.set("offset", String(query.offset));
    if (query?.needs_review != null) params.set("needs_review", String(query.needs_review));
    const qs = params.toString();
    return apiRequest<ReviewListResponse>(`/reviews${qs ? `?${qs}` : ""}`, { accessToken });
  },
  get(accessToken: string, id: string) {
    return apiRequest<ReviewDetail>(`/reviews/${id}`, { accessToken });
  },
  remove(accessToken: string, id: string) {
    return apiRequest<{ deleted: boolean }>(`/reviews/${id}`, {
      method: "DELETE",
      accessToken,
    });
  },
  feedback(
    accessToken: string,
    id: string,
    body: {
      consistency: DimJudgment;
      authenticity: DimJudgment;
      experience: DimJudgment;
      usefulness: DimJudgment;
      note?: string;
    },
  ) {
    return apiRequest<ReviewSummary>(`/reviews/${id}/feedback`, {
      method: "POST",
      accessToken,
      body,
    });
  },
  feedbackHints(accessToken: string, limit = 8) {
    return apiRequest<{ hints: FeedbackHint[] }>(`/reviews/feedback/hints?limit=${limit}`, {
      accessToken,
    });
  },
  analytics(accessToken: string, threshold?: number) {
    const qs =
      threshold != null && threshold > 0 ? `?threshold=${encodeURIComponent(String(threshold))}` : "";
    return apiRequest<Analytics>(`/reviews/analytics${qs}`, { accessToken });
  },
  games(accessToken: string) {
    return apiRequest<{ games: string[] }>("/games", { accessToken });
  },
  gameAverage(accessToken: string, gameName: string) {
    return apiRequest<GameAverage>(`/games/${encodeURIComponent(gameName)}/average`, {
      accessToken,
    });
  },
};

export type Analytics = {
  total_reviews: number;
  flagged_count: number;
  avg_trust_score: number;
  feedback_scored_count: number;
  avg_correctness_score: number;
  grade_counts: Record<string, number>;
  dimension_distribution: Record<string, Record<string, number>>;
  threshold: number;
  would_flag_at_threshold: number;
};

export type GameAverage = {
  game_name: string;
  review_count: number;
  raw_avg: number;
  weighted_avg: number;
  inflation: number;
};

export const adminApi = {
  getLLMConfig(accessToken: string) {
    return apiRequest<LLMRuntimeConfig>("/admin/llm-config", { accessToken });
  },
  patchLLMConfig(accessToken: string, body: Partial<LLMRuntimeConfig>) {
    return apiRequest<LLMRuntimeConfig>("/admin/llm-config", {
      method: "PATCH",
      accessToken,
      body,
    });
  },
  listAdapters(accessToken: string) {
    return apiRequest<{ adapters: AdapterMeta[] }>("/admin/adapters", { accessToken });
  },
  upsertAdapter(
    accessToken: string,
    body: { id: string; name: string; path?: string; description?: string },
  ) {
    return apiRequest<AdapterMeta>("/admin/adapters", {
      method: "POST",
      accessToken,
      body,
    });
  },
  removeAdapter(accessToken: string, id: string) {
    return apiRequest<{ deleted: boolean }>(`/admin/adapters/${encodeURIComponent(id)}`, {
      method: "DELETE",
      accessToken,
    });
  },
  activateAdapter(accessToken: string, adapter_id: string) {
    return apiRequest<LLMRuntimeConfig>("/admin/adapters/activate", {
      method: "POST",
      accessToken,
      body: { adapter_id },
    });
  },
  listLogs(accessToken: string, limit = 50) {
    return apiRequest<{ logs: QueryLogEntry[] }>(`/admin/logs?limit=${limit}`, { accessToken });
  },
  exportFinetune(accessToken: string) {
    return apiRequest<{ count: number; examples: unknown[] }>("/admin/finetune/export", {
      accessToken,
    });
  },
  async downloadFinetuneJsonl(accessToken: string) {
    const res = await fetch(`${getApiUrl()}/admin/finetune/export?format=jsonl`, {
      headers: { Authorization: `Bearer ${accessToken}`, Accept: "application/x-ndjson" },
    });
    if (!res.ok) {
      throw new Error(`Export failed (${res.status})`);
    }
    return res.text();
  },
};
