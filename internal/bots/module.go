package bots

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	postgresinfra "github.com/example/bots-platform/internal/infra/postgres"
	redisinfra "github.com/example/bots-platform/internal/infra/redis"
	"github.com/example/bots-platform/internal/jobs"
	"github.com/example/bots-platform/internal/registry"
	"github.com/example/bots-platform/internal/telegram"
)

type Module interface {
	Name() string
	HandleUpdate(ctx context.Context, bot *BotRuntime, update telegram.Update) error
}

type Dependencies struct {
	Logger   *slog.Logger
	Telegram *telegram.APIClient
	HTTP     *http.Client
	Redis    *redisinfra.Client
	Postgres *postgresinfra.Client
	Jobs     *jobs.Runner
}

type BotRuntime struct {
	Config *registry.BotConfig
	Module Module
	Deps   Dependencies
	Logger *slog.Logger
}

type ModuleRegistry struct {
	modules map[string]Module
}

func NewModuleRegistry(modules ...Module) (*ModuleRegistry, error) {
	registry := &ModuleRegistry{
		modules: make(map[string]Module, len(modules)),
	}

	for _, module := range modules {
		if module == nil {
			return nil, errors.New("module must not be nil")
		}
		if module.Name() == "" {
			return nil, errors.New("module name is required")
		}
		if _, exists := registry.modules[module.Name()]; exists {
			return nil, fmt.Errorf("module %q is already registered", module.Name())
		}
		registry.modules[module.Name()] = module
	}

	return registry, nil
}

func (r *ModuleRegistry) Get(name string) (Module, bool) {
	if r == nil {
		return nil, false
	}
	module, ok := r.modules[name]
	return module, ok
}

func (r *ModuleRegistry) Names() map[string]struct{} {
	names := make(map[string]struct{}, len(r.modules))
	for name := range r.modules {
		names[name] = struct{}{}
	}
	return names
}

