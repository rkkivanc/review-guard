# ReviewGuard — mf-backend

Go API for ReviewGuard. Layered clean architecture aligned with MasterFabric Academy days 46 / 56–57:

```
cmd/server          → thin main (wire + graceful shutdown)
internal/domain     → business types (no HTTP/SQL)
internal/service    → use cases
internal/repository → Store interface + memory + Postgres/pgxpool
internal/httpapi    → transport (chi handlers + response envelope)
internal/config     → environment configuration
```

Service packages depend on **narrow consumer-side interfaces** (`UserRepository`,
`ReviewRepository`, `HealthStore`) rather than the full `Store` surface.
## Run (local, zero DB setup)

```bash
go run ./cmd/server
```

With empty `DATABASE_URL` the file-backed memory repository is used (`DATA_DIR`, default `data/`).
Mutations update in-memory indexes under a short lock; persistence is coalesced asynchronously
(so write lock is never held across disk I/O). Users, sessions, and reviews survive process restarts.

With `DATABASE_URL` set, the Postgres/`pgxpool` repository is used (migrations applied on boot).
Pool defaults: MaxConns=20, MinConns=2, MaxConnLifetime=1h, MaxConnIdleTime=30m.

Smoke endpoints:
- `GET /health` · `GET /ready` · `GET /config` · `GET /version`

Auth (§5 #6–13) — bcrypt passwords, HS256 access JWT, rotating opaque refresh (sha256 at rest):
- `POST /auth/register` · `POST /auth/login` · `POST /auth/refresh` · `POST /auth/logout`
- `GET /auth/me` · `PATCH /auth/me` · `POST /auth/change-password` · `GET /auth/sessions`
- `GET /games` (auth; empty until reviews exist)

Reviews + decision scoring (§5 #14–21) — trust is **always recomputed server-side** on write/rescore:
- `POST /reviews` · `GET /reviews` · `GET /reviews/{id}` · `DELETE /reviews/{id}`
- `POST /reviews/{id}/rescore` · `GET /reviews/{id}/score`
- `GET /reviews/analytics?threshold=` · `GET /games/{name}/average`

Pure scoring logic lives in `internal/scoring` (unit-tested). Client-supplied trust is ignored.

Set `JWT_SECRET` in production. Local memory mode persists a secret under `DATA_DIR/jwt.secret` if unset.
