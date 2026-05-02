package echo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/bots-platform/internal/bots"
	"github.com/example/bots-platform/internal/registry"
	"github.com/example/bots-platform/internal/telegram"
)

func TestHandleUpdateStart(t *testing.T) {
	t.Parallel()

	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotText = body.Text
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	module := New()
	botRuntime := &bots.BotRuntime{
		Config: &registry.BotConfig{
			Manifest: registry.Manifest{
				WelcomeMessage: "hello from bot",
			},
			Secrets: registry.ResolvedSecrets{
				Token: "token",
			},
		},
		Deps: bots.Dependencies{
			Telegram: telegram.NewClient(server.URL, server.Client(), nil),
		},
	}

	err := module.HandleUpdate(context.Background(), botRuntime, telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			Text: "/start",
			Chat: telegram.Chat{ID: 10},
		},
	})
	if err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if gotText != "hello from bot" {
		t.Fatalf("unexpected text: %q", gotText)
	}
}

func TestHandleUpdateEcho(t *testing.T) {
	t.Parallel()

	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		gotText = body.Text
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	module := New()
	botRuntime := &bots.BotRuntime{
		Config: &registry.BotConfig{
			Secrets: registry.ResolvedSecrets{
				Token: "token",
			},
		},
		Deps: bots.Dependencies{
			Telegram: telegram.NewClient(server.URL, server.Client(), nil),
		},
	}

	err := module.HandleUpdate(context.Background(), botRuntime, telegram.Update{
		UpdateID: 1,
		Message: &telegram.Message{
			Text: "hello",
			Chat: telegram.Chat{ID: 10},
		},
	})
	if err != nil {
		t.Fatalf("HandleUpdate() error = %v", err)
	}
	if gotText != "Echo: hello" {
		t.Fatalf("unexpected text: %q", gotText)
	}
}
