package workbot

import (
	"context"
	"strings"

	"github.com/example/bots-platform/internal/bots"
	"github.com/example/bots-platform/internal/telegram"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "workbot" }

func (m *Module) HandleUpdate(ctx context.Context, bot *bots.BotRuntime, update telegram.Update) error {
	if !bot.Deps.Redis.Enabled() {
		bot.Logger.Error("redis is required for workbot but is not configured")
		return nil
	}

	if update.CallbackQuery != nil {
		return handleCallback(ctx, bot, update.CallbackQuery)
	}

	msg := update.Message
	if msg == nil {
		return nil
	}

	if msg.Contact != nil {
		store := NewRedisStore(bot.Deps.Redis.Raw())
		return handleContact(ctx, bot, msg, store)
	}

	if msg.Location != nil {
		store := NewRedisStore(bot.Deps.Redis.Raw())
		return handleLocation(ctx, bot, msg, store)
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}

	if strings.HasPrefix(text, "/start") {
		store := NewRedisStore(bot.Deps.Redis.Raw())
		return handleStart(ctx, bot, msg, store)
	}

	if text == "/myjobs" || text == "📋 Мои записи" {
		return handleMyJobs(ctx, bot, msg)
	}

	if text == "💼 Вакансии" {
		return handleVacancies(ctx, bot, msg)
	}

	if text == "👤 Профиль" {
		return handleProfile(ctx, bot, msg)
	}

	if text == "💬 Поддержка" {
		return handleSupport(ctx, bot, msg)
	}

	return handleText(ctx, bot, msg)
}
