# ReviewGuard — frontend

Next.js 15 App Router SPA — one shell + client-side master view router. See the [root README](../README.md) for product overview and full-stack setup.

## Master views

1. **Auth** — Login / Register (only when logged out)
2. **Simulator** — Review form · classification result (backend MLC LLM via API)
3. **Dashboard** — overview, classification history, per-game averages, grade legend, and correctness feedback

## Run

With the Docker stack (recommended):

```bash
# repo root
docker compose up --build

# UI
cp .env.example .env.local   # set NEXT_PUBLIC_API_URL=http://localhost:8088
npm install
npm run dev
```

Or point at a local API on `:8080` (must have `MLC_LLM_URL` configured).

Open [http://localhost:3000](http://localhost:3000).

## Notes

- Access JWT stays in memory; refresh token in `sessionStorage` (rotates on use).
- Classification is **server-side** (`POST /reviews` → backend → `mlc-llm` container). No `@mlc-ai/web-llm` / WebGPU required.
- UI is English-only; styling uses neo-brutalist tokens.
