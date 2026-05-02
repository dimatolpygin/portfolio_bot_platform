package bots

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/example/bots-platform/internal/telegram"
)

const webhookSecretHeader = "X-Telegram-Bot-Api-Secret-Token"

type Router struct {
	registry *RuntimeRegistry
	logger   *slog.Logger
}

func NewRouter(registry *RuntimeRegistry, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}

	return &Router{
		registry: registry,
		logger:   logger,
	}
}

func (r *Router) WebhookHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		slug := req.PathValue("bot_slug")
		bot, ok := r.registry.Get(slug)
		if !ok {
			r.logger.Warn("webhook for unknown bot", "bot_slug", slug)
			http.Error(w, "unknown bot", http.StatusNotFound)
			return
		}
		if !bot.Config.Manifest.IsEnabled() {
			r.logger.Warn("webhook for disabled bot", "bot_slug", slug)
			http.Error(w, "bot is disabled", http.StatusForbidden)
			return
		}
		if !validSecret(req.Header.Get(webhookSecretHeader), bot.Config.Secrets.WebhookSecret) {
			r.logger.Warn("webhook secret mismatch", "bot_slug", slug)
			http.Error(w, "invalid webhook secret", http.StatusUnauthorized)
			return
		}

		req.Body = http.MaxBytesReader(w, req.Body, 1<<20)

		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "failed to read update", http.StatusBadRequest)
			return
		}

		var update telegram.Update
		if err := json.Unmarshal(body, &update); err != nil {
			http.Error(w, "invalid update payload", http.StatusBadRequest)
			return
		}

		if err := bot.Module.HandleUpdate(req.Context(), bot, update); err != nil {
			bot.Logger.Error("failed to handle telegram update", "error", err, "update_id", update.UpdateID)
			http.Error(w, "failed to handle update", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"bot_slug": slug,
		})
	})
}

func validSecret(got, expected string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal response: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
