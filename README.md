<div align="center">

<a href="https://academy.masterfabric.co">
  <img src="https://academy.masterfabric.co/academy-badge.png" width="120" alt="MasterFabric Academy">
</a>

<p>
  <sub>
    academy.masterfabric.co is a
    <a href="https://masterfabric.co">MasterFabric</a>
    subsidiary.
  </sub>
</p>

</div>

# ReviewGuard

ReviewGuard scores how trustworthy a game review looks. Classification runs on a **local MLC LLM Docker service**; the Go API stores results and recomputes trust on the server. Grafana + Prometheus observe the API.

## How it works

1. You submit a game name, star rating, and review text in the **Simulator**.
2. The backend calls the **MLC LLM** container (OpenAI-compatible API) and classifies the review on four dimensions — typically three runs:
   - **Consistency** — aligned / mismatched (text vs stars)
   - **Authenticity** — genuine / suspicious / bot
   - **Experience** — experience_based / speculative
   - **Usefulness** — useful / neutral / empty
3. Trust (0–100) and grade A–F are computed **only on the server**.
4. The **Dashboard** shows history, analytics, per-game averages, and human correctness feedback.

```
┌──────────────┐   JWT    ┌────────────┐      ┌────────────┐
│ Next.js UI   │ ───────► │ gateway/LB │ ───► │ Go backend │
└──────────────┘          └────────────┘      └─────┬──────┘
                                                    │
                              ┌─────────────────────┼─────────────────────┐
                              ▼                     ▼                     ▼
                         mlc-llm              Prometheus              Grafana
                         (Docker)              (+ Loki)              (UI :3001)
```

## Repository layout

| Path | Role |
|------|------|
| [`frontend/`](frontend/) | Next.js 15 App Router UI |
| [`backend/`](backend/) | Go API (clean architecture) |
| [`services/mlc-llm/`](services/mlc-llm/) | Local MLC-compatible LLM container |
| [`infra/`](infra/) | Gateway, Prometheus, Grafana, Loki |
| [`k8s/`](k8s/) | Pod scaling / HPA manifests |
| [`docker-compose.yml`](docker-compose.yml) | Local full stack |

## Quick start (recommended — Docker)

```bash
docker compose up --build
# optional horizontal scale:
# docker compose up --build --scale backend=2
```

| Endpoint | URL |
|----------|-----|
| API (via gateway) | http://localhost:8088 |
| Grafana | http://localhost:3001 (admin/admin) |
| Prometheus | http://localhost:9090 |
| MLC LLM health | http://localhost:8000/health |

```bash
cd frontend
echo 'NEXT_PUBLIC_API_URL=http://localhost:8088' > .env.local
npm install && npm run dev
```

Open [http://localhost:3000](http://localhost:3000). Infrastructure details: [`infra/README.md`](infra/README.md).

## Dev without Compose (API only)

```bash
# terminal 1 — MLC LLM service
cd services/mlc-llm && pip install -r requirements.txt && uvicorn app:app --port 8000

# terminal 2 — API
cd backend
export MLC_LLM_URL=http://localhost:8000
go run ./cmd/server

# terminal 3 — UI
cd frontend && npm run dev
```

Postgres is optional. With an empty `DATABASE_URL`, the API uses a file-backed memory store under `backend/data/`.

## Trust grades

| Grade | Trust score |
|-------|-------------|
| A | 90–100 |
| B | 80–89 |
| C | 70–79 |
| D | 60–69 |
| F | 0–59 |

Default `TRUST_THRESHOLD` is **70**. Composite weights: authenticity 30%, experience 30%, consistency 25%, usefulness 15%.

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
| `CLASSIFICATION_RUNS` | `3` | MLC runs per review |
| `CLASSIFICATION_TEMPERATURE` | `0.7` | Passed to MLC |
| `MLC_LLM_URL` | _(empty)_ | e.g. `http://mlc-llm:8000` — **required** for classify |
| `MLC_MODEL_ID` | `gemma-2-2b-it-q4f16_1-MLC` | Model id sent to MLC |
| `LLM_TIMEOUT` | `60s` | Per-request client timeout |

### Frontend (`frontend/`)

| Variable | Default |
|----------|---------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` (use `http://localhost:8088` with compose gateway) |

## Docs

- [Backend README](backend/README.md) — architecture, endpoints, security
- [Frontend README](frontend/README.md) — views, local UI setup
- [Infra README](infra/README.md) — Docker Compose, Grafana, gateway, Kubernetes HPA
