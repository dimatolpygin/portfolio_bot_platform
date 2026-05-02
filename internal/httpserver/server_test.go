package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/bots-platform/internal/config"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	handler := NewHandler(config.ModeAll, 3, nil, http.NotFoundHandler(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	var body HealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if body.Status != "ok" || body.Mode != "all" || body.BotCount != 3 {
		t.Fatalf("unexpected health payload: %+v", body)
	}
}

