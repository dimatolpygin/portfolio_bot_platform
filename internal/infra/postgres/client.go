package postgres

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Client struct {
	db      *sql.DB
	enabled bool
	logger  *slog.Logger
}

func New(databaseURL string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if databaseURL == "" {
		logger.Info("postgres is not configured; continuing without shared postgres client")
		return &Client{logger: logger}, nil
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	logger.Info("shared postgres client configured")

	return &Client{
		db:      db,
		enabled: true,
		logger:  logger,
	}, nil
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled && c.db != nil
}

func (c *Client) DB() *sql.DB {
	if c == nil {
		return nil
	}
	return c.db
}

func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

