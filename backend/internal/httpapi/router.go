package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// Dependencies are the services the HTTP transport needs.
type Dependencies struct {
	Config  config.Config
	Tokens  *auth.TokenManager
	Health  *service.HealthService
	CfgSvc  *service.ConfigService
	AuthSvc *service.AuthService
}

// NewRouter builds the chi router (config + common + auth).
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Refresh-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	health := NewHealthHandler(deps.Health)
	cfg := NewConfigHandler(deps.CfgSvc)
	authH := NewAuthHandler(deps.AuthSvc)

	// Config module (§5 #1–2)
	r.Get("/config", cfg.Config)
	r.Get("/version", cfg.Version)

	// Common module (§5 #3–4)
	r.Get("/health", health.Live)
	r.Get("/ready", health.Ready)

	// Auth module (§5 #6–13) — public
	r.Post("/auth/register", authH.Register)
	r.Post("/auth/login", authH.Login)
	r.Post("/auth/refresh", authH.Refresh)
	r.Post("/auth/logout", authH.Logout)

	// Auth + common protected (§5 #5, #10–13)
	r.Group(func(pr chi.Router) {
		pr.Use(RequireAuth(deps.Tokens))
		pr.Get("/auth/me", authH.Me)
		pr.Patch("/auth/me", authH.UpdateMe)
		pr.Post("/auth/change-password", authH.ChangePassword)
		pr.Get("/auth/sessions", authH.Sessions)
		pr.Get("/games", authH.Games)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return r
}
