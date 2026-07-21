package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/httpapi"
	"github.com/masterfabric/review-guard/mf-backend/internal/repository"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.Load()
	ephemeralJWT, err := cfg.EnsureJWTSecret()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}
	if ephemeralJWT {
		slog.Warn("JWT_SECRET unset; generated ephemeral secret for memory-store local dev (sessions reset on restart)")
	}

	var store repository.Store
	if cfg.UsingMemoryStore() {
		slog.Info("DATABASE_URL empty; using in-memory repository")
		store = repository.NewMemoryStore()
	} else {
		slog.Warn("DATABASE_URL set but Postgres repository not wired yet; using in-memory repository")
		store = repository.NewMemoryStore()
	}

	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	if err != nil {
		slog.Error("token manager", "err", err)
		os.Exit(1)
	}

	healthSvc := service.NewHealthService(cfg, store)
	cfgSvc := service.NewConfigService(cfg)
	authSvc := service.NewAuthService(store, tokens)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:  cfg,
		Tokens:  tokens,
		Health:  healthSvc,
		CfgSvc:  cfgSvc,
		AuthSvc: authSvc,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("reviewguard listening", "addr", srv.Addr, "version", cfg.Version, "store", store.Name())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
