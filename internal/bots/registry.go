package bots

import (
	"fmt"
	"log/slog"

	"github.com/example/bots-platform/internal/registry"
)

type RuntimeRegistry struct {
	bots map[string]*BotRuntime
}

func NewRuntimeRegistry(store *registry.Store, deps Dependencies, modules *ModuleRegistry, logger *slog.Logger) (*RuntimeRegistry, error) {
	if store == nil {
		return nil, fmt.Errorf("registry store is required")
	}
	if modules == nil {
		return nil, fmt.Errorf("module registry is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	botsBySlug := make(map[string]*BotRuntime, store.Count())
	for _, botConfig := range store.List() {
		module, ok := modules.Get(botConfig.Manifest.Module)
		if !ok {
			return nil, fmt.Errorf("module %q is not registered for slug %q", botConfig.Manifest.Module, botConfig.Manifest.Slug)
		}

		botsBySlug[botConfig.Manifest.Slug] = &BotRuntime{
			Config: botConfig,
			Module: module,
			Deps:   deps,
			Logger: logger.With("bot_slug", botConfig.Manifest.Slug, "module", module.Name()),
		}
	}

	return &RuntimeRegistry{bots: botsBySlug}, nil
}

func (r *RuntimeRegistry) Get(slug string) (*BotRuntime, bool) {
	if r == nil {
		return nil, false
	}
	bot, ok := r.bots[slug]
	return bot, ok
}

func (r *RuntimeRegistry) Count() int {
	if r == nil {
		return 0
	}
	return len(r.bots)
}

