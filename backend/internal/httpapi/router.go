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
	Config    config.Config
	Tokens    *auth.TokenManager
	Health    *service.HealthService
	CfgSvc    *service.ConfigService
	AuthSvc   *service.AuthService
	ReviewSvc *service.ReviewService
}

// NewRouter builds the chi router (config + common + auth + reviews).
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
	reviewH := NewReviewHandler(deps.ReviewSvc)

	r.Get("/config", cfg.Config)
	r.Get("/version", cfg.Version)
	r.Get("/health", health.Live)
	r.Get("/ready", health.Ready)

	r.Post("/auth/register", authH.Register)
	r.Post("/auth/login", authH.Login)
	r.Post("/auth/refresh", authH.Refresh)
	r.Post("/auth/logout", authH.Logout)

	r.Group(func(pr chi.Router) {
		pr.Use(RequireAuth(deps.Tokens))

		pr.Get("/auth/me", authH.Me)
		pr.Patch("/auth/me", authH.UpdateMe)
		pr.Post("/auth/change-password", authH.ChangePassword)
		pr.Get("/auth/sessions", authH.Sessions)
		pr.Get("/games", authH.Games)
		pr.Get("/games/{name}/average", reviewH.GameAverage)

		// Static path before /reviews/{id}
		pr.Get("/reviews/analytics", reviewH.Analytics)
		pr.Post("/reviews", reviewH.Create)
		pr.Get("/reviews", reviewH.List)
		pr.Get("/reviews/{id}", reviewH.Get)
		pr.Delete("/reviews/{id}", reviewH.Delete)
		pr.Post("/reviews/{id}/rescore", reviewH.Rescore)
		pr.Get("/reviews/{id}/score", reviewH.Score)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return r
}
