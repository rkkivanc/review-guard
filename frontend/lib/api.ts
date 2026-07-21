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
  created_at: string;
};

export type AuthTokens = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: User;
};

const API_URL =
  process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") || "http://localhost:8080";

export function getApiUrl() {
  return API_URL;
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

type RequestOptions = {
  method?: string;
  body?: unknown;
  accessToken?: string | null;
  signal?: AbortSignal;
};

export async function apiRequest<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (options.accessToken) {
    headers.Authorization = `Bearer ${options.accessToken}`;
  }

  const res = await fetch(`${API_URL}${path}`, {
    method: options.method || "GET",
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
    signal: options.signal,
  });

  let envelope: Envelope<T>;
  try {
    envelope = (await res.json()) as Envelope<T>;
  } catch {
    throw new ApiClientError(res.status, "bad_response", "Server returned a non-JSON response.");
  }

  if (!envelope.success || envelope.data === null) {
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
    return apiRequest<AuthTokens>("/auth/register", { method: "POST", body });
  },
  login(body: { email: string; password: string }) {
    return apiRequest<AuthTokens>("/auth/login", { method: "POST", body });
  },
  refresh(refresh_token: string) {
    return apiRequest<AuthTokens>("/auth/refresh", {
      method: "POST",
      body: { refresh_token },
    });
  },
  logout(refresh_token: string) {
    return apiRequest<{ logged_out: boolean }>("/auth/logout", {
      method: "POST",
      body: { refresh_token },
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
      latency_ms: number;
      runs: ClassificationRunPayload[];
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
};
