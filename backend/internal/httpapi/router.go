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

	r.Use(SecurityHeaders)
	r.Use(TrustedProxies(deps.Config.TrustedProxies))
	r.Use(middleware.RequestID)
	r.Use(RedactSensitiveQuery)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Refresh-Token"},
		AllowCredentials: false, // Bearer tokens; cookies not used
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

	authLimit := NewRateLimiter(10, time.Minute) // login/register/refresh/logout
	r.Group(func(ar chi.Router) {
		ar.Use(authLimit.Middleware)
		ar.Post("/auth/register", authH.Register)
		ar.Post("/auth/login", authH.Login)
		ar.Post("/auth/refresh", authH.Refresh)
		ar.Post("/auth/logout", authH.Logout)
	})

	r.Group(func(pr chi.Router) {
		pr.Use(RequireAuth(deps.Tokens, deps.AuthSvc))

		pr.Get("/auth/me", authH.Me)
		pr.Patch("/auth/me", authH.UpdateMe)
		pr.Post("/auth/change-password", authH.ChangePassword)
		pr.Get("/auth/sessions", authH.Sessions)
		pr.Get("/games", authH.Games)
		pr.Get("/games/{name}/average", reviewH.GameAverage)

		pr.Get("/reviews/analytics", reviewH.Analytics)
		pr.Get("/reviews/feedback/hints", reviewH.FeedbackHints)
		pr.Post("/reviews", reviewH.Create)
		pr.Get("/reviews", reviewH.List)
		pr.Get("/reviews/{id}", reviewH.Get)
		pr.Delete("/reviews/{id}", reviewH.Delete)
		pr.Post("/reviews/{id}/rescore", reviewH.Rescore)
		pr.Get("/reviews/{id}/score", reviewH.Score)
		pr.Post("/reviews/{id}/feedback", reviewH.Feedback)
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return r
}
