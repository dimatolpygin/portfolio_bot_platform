package bots

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/bots-platform/internal/registry"
	"github.com/example/bots-platform/internal/telegram"
)

func TestWebhookHandlerStartMessage(t *testing.T) {
	t.Parallel()

	var sentText string
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sentText = readTextField(t, req)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer telegramServer.Close()

	botRuntime := &BotRuntime{
		Config: &registry.BotConfig{
			Manifest: registry.Manifest{
				Slug:           "sample-echo",
				Name:           "Sample Echo Bot",
				Description:    "Sample bot",
				Enabled:        boolPointer(true),
				Module:         "echo",
				WelcomeMessage: "Hello from sample bot",
				Telegram: registry.TelegramConfig{
					TokenEnv:         "BOT_TOKEN",
					WebhookSecretEnv: "BOT_SECRET",
				},
			},
			Secrets: registry.ResolvedSecrets{
				Token:         "bot-token",
				WebhookSecret: "hook-secret",
			},
		},
		Module: stubModule{},
		Deps: Dependencies{
			Telegram: telegram.NewClient(telegramServer.URL, telegramServer.Client(), nil),
		},
	}

	registry := &RuntimeRegistry{
		bots: map[string]*BotRuntime{
			"sample-echo": botRuntime,
		},
	}

	router := NewRouter(registry, nil)
	handler := router.WebhookHandler()

	req := httptest.NewRequest(http.MethodPost, "/telegram/sample-echo/webhook", bytes.NewBufferString(`{
	  "update_id": 1,
	  "message": {
	    "message_id": 2,
	    "text": "/start",
	    "chat": {"id": 99, "type": "private"}
	  }
	}`))
	req.Header.Set(webhookSecretHeader, "hook-secret")
	req.SetPathValue("bot_slug", "sample-echo")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", recorder.Code, recorder.Body.String())
	}
	if sentText != "Hello from sample bot" {
		t.Fatalf("unexpected sent text: %q", sentText)
	}
}

func TestWebhookHandlerRejectsBadSecret(t *testing.T) {
	t.Parallel()

	router := NewRouter(&RuntimeRegistry{
		bots: map[string]*BotRuntime{
			"sample-echo": {
				Config: &registry.BotConfig{
					Manifest: registry.Manifest{
						Slug:    "sample-echo",
						Enabled: boolPointer(true),
					},
					Secrets: registry.ResolvedSecrets{WebhookSecret: "hook-secret"},
				},
				Module: stubModule{},
				Deps: Dependencies{
					Telegram: telegram.NewClient("http://example.com", http.DefaultClient, nil),
				},
			},
		},
	}, nil)

	req := httptest.NewRequest(http.MethodPost, "/telegram/sample-echo/webhook", bytes.NewBufferString(`{"update_id":1}`))
	req.Header.Set(webhookSecretHeader, "wrong-secret")
	req.SetPathValue("bot_slug", "sample-echo")

	recorder := httptest.NewRecorder()
	router.WebhookHandler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func TestWebhookHandlerUnknownBot(t *testing.T) {
	t.Parallel()

	router := NewRouter(&RuntimeRegistry{bots: map[string]*BotRuntime{}}, nil)

	req := httptest.NewRequest(http.MethodPost, "/telegram/unknown/webhook", bytes.NewBufferString(`{"update_id":1}`))
	req.Header.Set(webhookSecretHeader, "whatever")
	req.SetPathValue("bot_slug", "unknown")

	recorder := httptest.NewRecorder()
	router.WebhookHandler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func readTextField(t *testing.T, req *http.Request) string {
	t.Helper()

	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return body.Text
}

type stubModule struct{}

func (stubModule) Name() string {
	return "stub"
}

func (stubModule) HandleUpdate(ctx context.Context, bot *BotRuntime, update telegram.Update) error {
	if update.Message == nil {
		return nil
	}

	text := update.Message.Text
	if text == "/start" {
		text = bot.Config.Manifest.WelcomeMessage
	} else {
		text = fmt.Sprintf("stub: %s", text)
	}

	return bot.Deps.Telegram.SendMessage(ctx, bot.Config.Secrets.Token, update.Message.Chat.ID, text)
}
