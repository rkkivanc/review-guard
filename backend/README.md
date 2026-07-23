# ReviewGuard — backend

Go API for ReviewGuard. See the [root README](../README.md) for product overview and full-stack setup.

## Architecture

Layered clean architecture:

```
cmd/server          → thin main (wire + graceful shutdown)
internal/domain     → business types (no HTTP/SQL)
internal/service    → use cases
internal/repository → Store interface + memory + Postgres/pgxpool
internal/httpapi    → transport (chi handlers + response envelope)
internal/scoring    → pure trust scoring (unit-tested)
internal/auth       → JWT + refresh helpers
internal/config     → environment configuration
```

Service packages depend on **narrow consumer-side interfaces** (`UserRepository`, `ReviewRepository`, `HealthStore`) rather than the full `Store` surface.

## Run

```bash
go run ./cmd/server
```

- Empty `DATABASE_URL` → file-backed memory repository (`DATA_DIR`, default `data/`). Mutations update in-memory indexes under a short lock; persistence is coalesced asynchronously so the write lock is never held across disk I/O. Users, sessions, and reviews survive restarts.
- With `DATABASE_URL` set → Postgres/`pgxpool` (migrations applied on boot). Pool defaults: MaxConns=20, MinConns=2, MaxConnLifetime=1h, MaxConnIdleTime=30m.

Set `JWT_SECRET` in production. Local memory mode persists a secret under `DATA_DIR/jwt.secret` if unset.

## Endpoints

Smoke:

- `GET /health` · `GET /ready` · `GET /config` · `GET /version`

Auth — bcrypt passwords, HS256 access JWT, rotating opaque refresh (sha256 at rest):

- `POST /auth/register` · `POST /auth/login` · `POST /auth/refresh` · `POST /auth/logout`
- `GET /auth/me` · `PATCH /auth/me` · `POST /auth/change-password` · `GET /auth/sessions`
- `GET /games` (auth; empty until reviews exist)

Reviews + decision scoring — trust is **always recomputed server-side** on write/rescore; client-supplied trust is ignored:

- `POST /reviews` · `GET /reviews` · `GET /reviews/{id}` · `DELETE /reviews/{id}`
- `POST /reviews/{id}/rescore` · `GET /reviews/{id}/score`
- `GET /reviews/analytics?threshold=` · `GET /games/{name}/average`

## Security

- Auth routes rate-limited (10/min/IP)
- Access JWT carries `tv` (token_version); password change / refresh reuse bumps it
- Refresh rotation is atomic (`ConsumeRefreshToken`); reuse revokes all sessions
- Pagination capped (`limit≤100`, `offset≤10000`); body size limits on auth/review JSON
- `X-Refresh-Token` header only (no query-string secrets); security headers enabled
- Optional `TRUSTED_PROXIES` CIDR list for `X-Forwarded-For`
