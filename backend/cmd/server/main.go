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

	"github.com/masterfabric/review-guard/mf-backend/internal/adapters"
	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/config"
	"github.com/masterfabric/review-guard/mf-backend/internal/httpapi"
	"github.com/masterfabric/review-guard/mf-backend/internal/llm"
	"github.com/masterfabric/review-guard/mf-backend/internal/llqlog"
	"github.com/masterfabric/review-guard/mf-backend/internal/llmruntime"
	"github.com/masterfabric/review-guard/mf-backend/internal/mcp"
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

	ctx := context.Background()
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
		ps, err := repository.OpenPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("open postgres store", "err", err)
			os.Exit(1)
		}
		slog.Info("using postgres repository")
		store = ps
	}
	defer func() {
		if err := store.Close(); err != nil {
			slog.Error("store close", "err", err)
		}
	}()

	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	if err != nil {
		slog.Error("token manager", "err", err)
		os.Exit(1)
	}

	adapterReg, err := adapters.New(cfg.PEFTAdaptersDir)
	if err != nil {
		slog.Error("adapters registry", "err", err)
		os.Exit(1)
	}
	runtimeStore := llmruntime.New(cfg.DefaultSystemPrompt, cfg.ClassificationTemperature, 1024, 1.0)
	queryLogs := llqlog.New(200)

	healthSvc := service.NewHealthService(cfg, store)
	cfgSvc := service.NewConfigService(cfg)
	authSvc := service.NewAuthService(store, tokens, cfg.IsAdminEmail)
	llmClient := llm.NewClient(cfg.MLCLLMURL, cfg.MLCModelID, cfg.ClassificationRuns, cfg.ClassificationTemperature, cfg.LLMTimeout)
	if llmClient.Enabled() {
		slog.Info("MLC LLM client configured", "url", cfg.MLCLLMURL, "model", cfg.MLCModelID)
	} else {
		slog.Warn("MLC_LLM_URL unset; POST /reviews will fail until the LLM service is configured")
	}
	reviewSvc := service.NewReviewService(store, cfg, llmClient, runtimeStore)
	adminSvc := service.NewAdminService(runtimeStore, adapterReg, queryLogs, llmClient, store)
	mcpHandler := &mcp.Handler{
		Tokens:   tokens,
		AuthSvc:  authSvc,
		LLM:      llmClient,
		Runtime:  runtimeStore,
		Adapters: adapterReg,
		Logs:     queryLogs,
	}

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:    cfg,
		Tokens:    tokens,
		Health:    healthSvc,
		CfgSvc:    cfgSvc,
		AuthSvc:   authSvc,
		ReviewSvc: reviewSvc,
		AdminSvc:  adminSvc,
		MCP:       mcpHandler,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
