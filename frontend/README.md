# ReviewGuard frontend

Next.js App Router SPA — one shell + client-side master view router.

## Master views
1. **Auth** — Login · Register · Profile (live against Go JWT API)
2. **Simulator** — stub (web-llm + Gemma next)
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
- LLM stays 100% in-browser later via `@mlc-ai/web-llm` (`gemma-2-2b-it-q4f16_1-MLC`).
- English-only UI, neo-brutalist tokens from the product brief.
