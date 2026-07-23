# ReviewGuard

ReviewGuard scores how trustworthy a game review looks. Classification runs entirely in the browser; the Go API stores results and recomputes trust on the server.

## How it works

1. You submit a game name, star rating, and review text in the **Simulator**.
2. An in-browser LLM ([WebLLM](https://webllm.mlc.ai/) + Gemma 2 2B) labels the review on four dimensions — typically three runs for stability:
   - **Consistency** — aligned / mismatched (text vs stars)
   - **Authenticity** — genuine / suspicious / bot
   - **Experience** — experience_based / speculative
   - **Usefulness** — useful / neutral / empty
3. The backend ignores any client-supplied trust value and recomputes a **0–100 trust score** (and grade A–F) from those runs.
4. The **Dashboard** shows history, analytics, per-game averages, and human correctness feedback that can improve later prompts.

```
┌─────────────────────┐         ┌─────────────────────┐
│  frontend (Next.js) │  JWT    │  backend (Go/chi)    │
│  WebLLM + Gemma     │ ──────► │  auth · reviews      │
│  Simulator / Dash   │ ◄────── │  scoring · store     │
└─────────────────────┘         └─────────────────────┘
```

## Repository layout

| Path | Role |
|------|------|
| [`frontend/`](frontend/) | Next.js 15 App Router UI |
| [`backend/`](backend/) | Go API (clean architecture) |

## Prerequisites

- **Go** 1.26+
- **Node.js** 20+ (npm)
- **WebGPU** browser for the simulator (Chrome / Edge 113+)

Postgres is optional. With an empty `DATABASE_URL`, the API uses a file-backed memory store under `backend/data/`.

## Quick start

```bash
# terminal 1 — API (port 8080)
cd backend
go run ./cmd/server

# terminal 2 — UI (port 3000)
cd frontend
cp .env.example .env.local   # optional; defaults to http://localhost:8080
npm install
npm run dev
```

Open [http://localhost:3000](http://localhost:3000).

First model download is ~1.5 GB and is cached by the browser afterward.

## Trust grades

| Grade | Trust score |
|-------|-------------|
| A | 90–100 |
| B | 80–89 |
| C | 70–79 |
| D | 60–69 |
| F | 0–59 |

Default `TRUST_THRESHOLD` is **70** — below that, `needs_review` is set. Composite weights: authenticity 30%, experience 30%, consistency 25%, usefulness 15%.

## Environment

### Backend (`backend/`)

| Variable | Default | Notes |
|----------|---------|--------|
| `PORT` | `8080` | Listen port |
| `DATABASE_URL` | _(empty)_ | Empty → memory+file store; set → Postgres |
| `DATA_DIR` | `data` | Memory-store persistence |
| `JWT_SECRET` | auto in memory mode | **Required** when using Postgres |
| `CORS_ORIGINS` | `http://localhost:3000` | Comma-separated |
| `TRUST_THRESHOLD` | `70` | Needs-review cutoff |
| `CLASSIFICATION_RUNS` | `3` | Exposed via `GET /config` for the UI |
| `CLASSIFICATION_TEMPERATURE` | `0.7` | Same |

### Frontend (`frontend/`)

| Variable | Default |
|----------|---------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` |

## Docs

- [Backend README](backend/README.md) — architecture, endpoints, security
- [Frontend README](frontend/README.md) — views, LLM notes, local UI setup
