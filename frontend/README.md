# ReviewGuard frontend

Next.js App Router SPA — one shell + client-side master view router.

## Master views
1. **Auth** — Login / Register (only when logged out)
2. **Simulator** — Model loader · Review form · Classification result (`@mlc-ai/web-llm` + Gemma)
3. **Dashboard** — stub (trust monitoring next)

## Run

```bash
# terminal 1 — API
cd ../backend && go run ./cmd/server

# terminal 2 — UI
cp .env.example .env.local   # optional; defaults to localhost:8080
npm run dev
```

Open http://localhost:3000

## Notes
- Access JWT stays in memory; refresh token in `sessionStorage` (rotates on use).
- LLM runs 100% in-browser via `@mlc-ai/web-llm` (`gemma-2-2b-it-q4f16_1-MLC`). Backend never calls a model.
- First model load ~1.5 GB (cached). Needs WebGPU (Chrome/Edge 113+).
- English-only UI, neo-brutalist tokens from the product brief.
