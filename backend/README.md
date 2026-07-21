# ReviewGuard — mf-backend

Go API for ReviewGuard. Layered clean architecture aligned with MasterFabric Academy days 46 / 56–57:

```
cmd/server          → thin main (wire + graceful shutdown)
internal/domain     → business types (no HTTP/SQL)
internal/service    → use cases
internal/repository → Store interface + memory (Postgres/pgx later)
internal/httpapi    → transport (chi handlers + response envelope)
internal/config     → environment configuration
```

## Run (local, zero DB setup)

```bash
go run ./cmd/server
```

With empty `DATABASE_URL` the in-memory repository is used automatically.

Smoke endpoints:
- `GET /health` · `GET /ready` · `GET /config` · `GET /version`

Auth (§5 #6–13) — bcrypt passwords, HS256 access JWT, rotating opaque refresh (sha256 at rest):
- `POST /auth/register` · `POST /auth/login` · `POST /auth/refresh` · `POST /auth/logout`
- `GET /auth/me` · `PATCH /auth/me` · `POST /auth/change-password` · `GET /auth/sessions`
- `GET /games` (auth; empty until reviews exist)

Set `JWT_SECRET` in production. Local memory mode generates an ephemeral secret if unset.

All responses use the §5 envelope: `{ "success", "data", "error" }`.
