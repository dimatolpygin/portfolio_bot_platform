package echo

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/bots-platform/internal/bots"
	"github.com/example/bots-platform/internal/telegram"
)

type Module struct{}

func New() *Module {
	return &Module{}
}

func (m *Module) Name() string {
	return "echo"
}

func (m *Module) HandleUpdate(ctx context.Context, bot *bots.BotRuntime, update telegram.Update) error {
	if update.Message == nil || strings.TrimSpace(update.Message.Text) == "" {
		bot.Logger.Debug("ignoring non-text telegram update", "update_id", update.UpdateID)
		return nil
	}

	text := strings.TrimSpace(update.Message.Text)
	reply := fmt.Sprintf("Echo: %s", text)
	if strings.HasPrefix(text, "/start") {
		reply = bot.Config.Manifest.WelcomeMessage
	}

	return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, update.Message.Chat.ID, reply)
}

