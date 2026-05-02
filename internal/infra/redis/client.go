package redis

import (
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"
)

type Client struct {
	raw     *goredis.Client
	enabled bool
	logger  *slog.Logger
}

func New(redisURL string, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if redisURL == "" {
		logger.Info("redis is not configured; continuing without shared redis client")
		return &Client{logger: logger}, nil
	}

	options, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	client := goredis.NewClient(options)
	logger.Info("shared redis client configured")

	return &Client{
		raw:     client,
		enabled: true,
		logger:  logger,
	}, nil
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled && c.raw != nil
}

func (c *Client) Raw() *goredis.Client {
	if c == nil {
		return nil
	}
	return c.raw
}

func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}

	return c.raw.Close()
}

