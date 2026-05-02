package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotBody sendMessageRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), nil)
	if err := client.SendMessage(context.Background(), "bot-token", 42, "hello"); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	if gotPath != "/botbot-token/sendMessage" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotBody.ChatID != 42 || gotBody.Text != "hello" {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestSendMessageReturnsTelegramError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"description":"bad request"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), nil)
	if err := client.SendMessage(context.Background(), "bot-token", 42, "hello"); err == nil {
		t.Fatalf("expected SendMessage() to fail")
	}
}

