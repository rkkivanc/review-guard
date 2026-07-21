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
