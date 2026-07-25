# MLC LLM service (local)

OpenAI-compatible LLM endpoint used by the Go backend (`MLC_LLM_URL`).

## Profiles

| Mode | How | Notes |
|------|-----|--------|
| **stub** (default) | `docker compose up --build` | FastAPI heuristic classifier; no GPU |
| **gpu** | `docker compose stop mlc-llm && docker compose --profile gpu up` | Real `ghcr.io/mlc-ai/mlc-llm` + NVIDIA device |

## Volumes

- `./models` → `/models` — place compiled MLC model weights / `lib.so` here for the GPU profile
- `./peft-adapters` → `/adapters` — LoRA/QLoRA adapter files + optional `registry.json`

## GPU example model path

```bash
# After downloading / compiling a model into ./models/FinalBoss-7B-q4f16_1
docker compose --profile gpu up
```

The GPU service command expects:

- Model: `/models/FinalBoss-7B-q4f16_1`
- Model lib: `/models/FinalBoss-7B/lib.so` (adjust compose `command` if your layout differs)

## Stub contract

- `GET /health`
- `GET /v1/models`
- `POST /v1/chat/completions` — OpenAI-compatible; supports `temperature`, `top_p`, `max_tokens`, `adapter_id`
- `GET /v1/adapters` — list adapters under `/adapters`
- `POST /v1/adapters/activate` — hot-swap active adapter id (stub bias)

## Host-only (no Docker / no pip)

When Docker Desktop is off, run the stdlib stub (Python 3, zero deps):

```bash
cd services/mlc-llm
python3 serve_local.py
```

Then restart the Go API (`go run ./cmd/server`). It defaults to `MLC_LLM_URL=http://localhost:8000`.

## Backend wiring

```env
MLC_LLM_URL=http://localhost:8000   # host go run
# MLC_LLM_URL=http://llm:8000       # docker compose
MLC_MODEL_ID=gemma-2-2b-it-q4f16_1-MLC
```
