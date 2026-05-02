package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/example/bots-platform/internal/admin"
	"github.com/example/bots-platform/internal/bots"
	metricsinfra "github.com/example/bots-platform/internal/metrics"
	"github.com/example/bots-platform/internal/bots/modules/echo"
	"github.com/example/bots-platform/internal/bots/modules/workbot"
	"github.com/example/bots-platform/internal/config"
	"github.com/example/bots-platform/internal/httpserver"
	"github.com/example/bots-platform/internal/infra/httpclient"
	postgresinfra "github.com/example/bots-platform/internal/infra/postgres"
	redisinfra "github.com/example/bots-platform/internal/infra/redis"
	"github.com/example/bots-platform/internal/jobs"
	"github.com/example/bots-platform/internal/logging"
	"github.com/example/bots-platform/internal/registry"
	"github.com/example/bots-platform/internal/telegram"
)

func Run(ctx context.Context, cfg config.Config) error {
	appStartTime := time.Now()
	logger := logging.New(cfg.LogLevel)
	logger.Info("starting bots-platform", "mode", cfg.Mode, "addr", cfg.HTTPAddr)

	sharedHTTPClient := httpclient.New(cfg.ExternalAPITimeout)
	telegramClient := telegram.NewClient(cfg.TelegramAPIBaseURL, sharedHTTPClient, logger.With("component", "telegram"))

	redisClient, err := redisinfra.New(cfg.RedisURL, logger.With("component", "redis"))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			logger.Warn("failed to close redis client", "error", closeErr)
		}
	}()

	postgresClient, err := postgresinfra.New(cfg.PostgresURL, logger.With("component", "postgres"))
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := postgresClient.Close(); closeErr != nil {
			logger.Warn("failed to close postgres client", "error", closeErr)
		}
	}()

	jobRunner := jobs.NewRunner(logger.With("component", "jobs"), cfg.WorkerBuffer)

	moduleRegistry, err := bots.NewModuleRegistry(echo.New(), workbot.New())
	if err != nil {
		return err
	}

	store, err := registry.Load(registry.LoadOptions{
		CatalogPath:    cfg.RegistryConfigPath,
		LockPath:       cfg.RegistryLockPath,
		BotsDir:        cfg.RegistryBotsDir,
		AllowedModules: moduleRegistry.Names(),
		Logger:         logger,
	})
	if err != nil {
		return err
	}

	runtimeRegistry, err := bots.NewRuntimeRegistry(store, bots.Dependencies{
		Logger:   logger,
		Telegram: telegramClient,
		HTTP:     sharedHTTPClient,
		Redis:    redisClient,
		Postgres: postgresClient,
		Jobs:     jobRunner,
	}, moduleRegistry, logger)
	if err != nil {
		return err
	}

	var (
		httpServer *httpserver.Server
		serverErr  = make(chan error, 1)
	)

	if cfg.Mode.RunsWorkers() {
		jobRunner.Start(ctx)
		if redisClient.Enabled() {
			go workbot.StartVacancyExpirer(ctx, redisClient.Raw(), logger.With("component", "vacancy-expirer"))
		}
	}

	if cfg.Mode.ServesHTTP() {
		router := bots.NewRouter(runtimeRegistry, logger.With("component", "telegram-webhook"))

		var adminHandler *admin.Handler
		adminToken := strings.TrimSpace(os.Getenv("WORKBOT_ADMIN_TOKEN"))
		if redisClient.Enabled() && adminToken != "" {
			if wb, ok := runtimeRegistry.Get("workbot"); ok {
				superAdminLogin := strings.TrimSpace(os.Getenv("WORKBOT_SUPER_LOGIN"))
				if superAdminLogin == "" {
					superAdminLogin = "admin"
				}
				adminHandler, err = admin.NewHandler(
					redisClient,
					telegramClient,
					sharedHTTPClient,
					wb.Config.Secrets.Token,
					adminToken,
					superAdminLogin,
					logger.With("component", "admin"),
				)
				if err != nil {
					return fmt.Errorf("init admin handler: %w", err)
				}
			}
		}

		if metricsAddr := strings.TrimSpace(os.Getenv("METRICS_ADDR")); metricsAddr != "" {
			var botList []metricsinfra.BotInfo
			for _, bc := range store.List() {
				botList = append(botList, metricsinfra.BotInfo{
					Slug:    bc.Catalog.Slug,
					Module:  bc.Manifest.Module,
					Repo:    bc.Catalog.Repo,
					Enabled: bc.Manifest.IsEnabled(),
				})
			}
			ms := metricsinfra.New(metricsAddr, appStartTime, botList, logger.With("component", "metrics"))
			ms.Start(ctx)
			defer func() {
				shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_ = ms.Shutdown(shutCtx)
			}()
		}

		handler := httpserver.NewHandler(cfg.Mode, runtimeRegistry.Count(), logger.With("component", "http"), router.WebhookHandler(), adminHandler)
		httpServer = httpserver.New(cfg, logger.With("component", "http"), handler)

		go func() {
			if err := httpServer.ListenAndServe(); err != nil {
				serverErr <- err
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case runErr = <-serverErr:
		logger.Error("http server stopped unexpectedly", "error", runErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if httpServer != nil {
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if runErr == nil {
				runErr = fmt.Errorf("shutdown http server: %w", err)
			}
			logger.Error("graceful http shutdown failed", "error", err)
		}
	}

	if err := jobRunner.Stop(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		if runErr == nil {
			runErr = fmt.Errorf("stop worker runner: %w", err)
		}
		logger.Error("graceful worker shutdown failed", "error", err)
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	logger.Info("bots-platform stopped")
	return nil
}
