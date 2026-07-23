-- ReviewGuard initial schema
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    token_version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT refresh_tokens_hash_unique UNIQUE (token_hash)
);

CREATE INDEX IF NOT EXISTS refresh_tokens_user_id_idx ON refresh_tokens (user_id);

CREATE TABLE IF NOT EXISTS reviews (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    game_name TEXT NOT NULL,
    stars INT NOT NULL CHECK (stars BETWEEN 1 AND 10),
    review_text TEXT NOT NULL,
    trust_score DOUBLE PRECISION NOT NULL,
    grade TEXT NOT NULL CHECK (grade IN ('A', 'B', 'C', 'D', 'F')),
    needs_review BOOLEAN NOT NULL DEFAULT FALSE,
    latency_ms INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    feedback JSONB,
    correctness_score DOUBLE PRECISION,
    correctness_label TEXT
);

CREATE INDEX IF NOT EXISTS reviews_user_created_idx ON reviews (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS reviews_user_game_idx ON reviews (user_id, lower(game_name));

CREATE TABLE IF NOT EXISTS classifications (
    id UUID PRIMARY KEY,
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    run_index INT NOT NULL CHECK (run_index >= 0),
    payload JSONB NOT NULL,
    CONSTRAINT classifications_review_run_unique UNIQUE (review_id, run_index)
);

CREATE INDEX IF NOT EXISTS classifications_review_id_idx ON classifications (review_id);

CREATE TABLE IF NOT EXISTS score_breakdown (
    id UUID PRIMARY KEY,
    review_id UUID NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL,
    final_label TEXT NOT NULL,
    agreement DOUBLE PRECISION NOT NULL,
    avg_confidence DOUBLE PRECISION NOT NULL,
    dim_score DOUBLE PRECISION NOT NULL,
    CONSTRAINT score_breakdown_review_dim_unique UNIQUE (review_id, dimension)
);

CREATE INDEX IF NOT EXISTS score_breakdown_review_id_idx ON score_breakdown (review_id);
