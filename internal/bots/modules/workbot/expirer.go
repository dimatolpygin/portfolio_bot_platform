package workbot

import (
	"context"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func StartVacancyExpirer(ctx context.Context, rdb *goredis.Client, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expireVacancies(ctx, rdb, logger)
		}
	}
}

func expireVacancies(ctx context.Context, rdb *goredis.Client, logger *slog.Logger) {
	vacancies, _ := ListAllVacancies(ctx, rdb)
	now := time.Now().UTC()
	for _, v := range vacancies {
		if v.Status != "active" || v.StartAt.IsZero() {
			continue
		}
		if now.After(v.StartAt) {
			if err := CloseVacancy(ctx, rdb, v.ID); err != nil {
				logger.Warn("vacancy auto-close failed", "id", v.ID, "error", err)
				continue
			}
			logger.Info("vacancy auto-closed", "id", v.ID, "city", v.City)
		}
	}
}
