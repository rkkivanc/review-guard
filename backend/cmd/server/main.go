package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	jwtFromFile, err := cfg.EnsureJWTSecret()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}
	if jwtFromFile && strings.TrimSpace(os.Getenv("JWT_SECRET")) == "" {
		slog.Info("JWT_SECRET unset; using persisted secret in data dir", "dir", cfg.DataDir)
	}

	var store repository.Store
	if cfg.UsingMemoryStore() {
		ms, err := repository.OpenMemoryStore(cfg.DataDir)
		if err != nil {
			slog.Error("open memory store", "err", err)
			os.Exit(1)
		}
		slog.Info("DATABASE_URL empty; using file-backed memory repository", "dir", cfg.DataDir)
		store = ms
	} else {
		slog.Warn("DATABASE_URL set but Postgres repository not wired yet; using file-backed memory repository")
		ms, err := repository.OpenMemoryStore(cfg.DataDir)
		if err != nil {
			slog.Error("open memory store", "err", err)
			os.Exit(1)
		}
		store = ms
	}

	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	if err != nil {
		slog.Error("token manager", "err", err)
		os.Exit(1)
	}

	healthSvc := service.NewHealthService(cfg, store)
	cfgSvc := service.NewConfigService(cfg)
	authSvc := service.NewAuthService(store, tokens)
	reviewSvc := service.NewReviewService(store, cfg)

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:    cfg,
		Tokens:    tokens,
		Health:    healthSvc,
		CfgSvc:    cfgSvc,
		AuthSvc:   authSvc,
		ReviewSvc: reviewSvc,
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
