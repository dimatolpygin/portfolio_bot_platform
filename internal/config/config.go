package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeServer Mode = "server"
	ModeWorker Mode = "worker"
	ModeAll    Mode = "all"
)

type Config struct {
	Mode               Mode
	HTTPAddr           string
	LogLevel           string
	TelegramAPIBaseURL string
	ExternalAPITimeout time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	ShutdownTimeout    time.Duration
	RegistryConfigPath string
	RegistryLockPath   string
	RegistryBotsDir    string
	RedisURL           string
	PostgresURL        string
	WorkerBuffer       int
}

func LoadFromEnv() (Config, error) {
	cfg := Config{
		Mode:               Mode(getEnv("APP_MODE", string(ModeAll))),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		LogLevel:           getEnv("LOG_LEVEL", "INFO"),
		TelegramAPIBaseURL: getEnv("TELEGRAM_API_BASE_URL", "https://api.telegram.org"),
		RegistryConfigPath: getEnv("REGISTRY_CONFIG_PATH", "config/bots.yml"),
		RegistryLockPath:   getEnv("REGISTRY_LOCK_PATH", "registry/bots.lock.json"),
		RegistryBotsDir:    getEnv("REGISTRY_BOTS_DIR", "registry/bots"),
		RedisURL:           os.Getenv("REDIS_URL"),
		PostgresURL:        os.Getenv("POSTGRES_URL"),
	}

	var err error

	if cfg.ExternalAPITimeout, err = getDuration("EXTERNAL_API_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ReadTimeout, err = getDuration("HTTP_READ_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WriteTimeout, err = getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.IdleTimeout, err = getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.WorkerBuffer, err = getInt("WORKER_BUFFER", 32); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (m Mode) Valid() bool {
	switch m {
	case ModeServer, ModeWorker, ModeAll:
		return true
	default:
		return false
	}
}

func (m Mode) ServesHTTP() bool {
	return m == ModeServer || m == ModeAll
}

func (m Mode) RunsWorkers() bool {
	return m == ModeWorker || m == ModeAll
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return value, nil
}

func getInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return value, nil
}

