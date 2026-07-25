package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/mcp"
	"github.com/masterfabric/review-guard/mf-backend/internal/metrics"
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
	AdminSvc  *service.AdminService
	MCP       *mcp.Handler
}

// NewRouter builds the chi router (config + common + auth + reviews + mcp + admin).
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Use(SecurityHeaders)
	r.Use(TrustedProxies(deps.Config.TrustedProxies))
	r.Use(middleware.RequestID)
	r.Use(RedactSensitiveQuery)
	r.Use(JSONRequestLogger)
	r.Use(metrics.Middleware)
	r.Use(middleware.Recoverer)
	// Classification can wait on MLC LLM (N runs).
	r.Use(middleware.Timeout(120 * time.Second))
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
	adminH := NewAdminHandler(deps.AdminSvc)

	r.Get("/config", cfg.Config)
	r.Get("/version", cfg.Version)
	r.Get("/health", health.Live)
	r.Get("/ready", health.Ready)
	r.Handle("/metrics", metrics.Handler())

	authLimit := NewRateLimiter(10, time.Minute) // login/register/refresh/logout
	r.Group(func(ar chi.Router) {
		ar.Use(authLimit.Middleware)
		ar.Post("/auth/register", authH.Register)
		ar.Post("/auth/login", authH.Login)
		ar.Post("/auth/refresh", authH.Refresh)
		ar.Post("/auth/logout", authH.Logout)
	})

	if deps.MCP != nil {
		r.Post("/mcp", deps.MCP.ServeHTTP)
	}

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

		if deps.AdminSvc != nil {
			pr.Group(func(ar chi.Router) {
				ar.Use(RequireAdmin(deps.AuthSvc))
				ar.Get("/admin/llm-config", adminH.GetLLMConfig)
				ar.Patch("/admin/llm-config", adminH.PatchLLMConfig)
				ar.Get("/admin/adapters", adminH.ListAdapters)
				ar.Post("/admin/adapters", adminH.UpsertAdapter)
				ar.Delete("/admin/adapters/{id}", adminH.DeleteAdapter)
				ar.Post("/admin/adapters/activate", adminH.ActivateAdapter)
				ar.Get("/admin/logs", adminH.ListLogs)
				ar.Get("/admin/finetune/export", adminH.ExportFinetune)
			})
		}
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusNotFound, "not_found", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return r
}

// JSONRequestLogger emits structured request logs for Grafana/Loki.
func JSONRequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}
