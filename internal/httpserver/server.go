package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/bots-platform/internal/admin"
	"github.com/example/bots-platform/internal/config"
)

type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

type HealthResponse struct {
	Status   string `json:"status"`
	Mode     string `json:"mode"`
	BotCount int    `json:"bot_count"`
}

func New(cfg config.Config, logger *slog.Logger, handler http.Handler) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{
		httpServer: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
		},
		logger: logger,
	}
}

func NewHandler(mode config.Mode, botCount int, logger *slog.Logger, telegramWebhookHandler http.Handler, adminHandler *admin.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, req *http.Request) {
		body, err := json.Marshal(HealthResponse{
			Status:   "ok",
			Mode:     string(mode),
			BotCount: botCount,
		})
		if err != nil {
			http.Error(w, "failed to build health response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	})
	mux.Handle("POST /telegram/{bot_slug}/webhook", telegramWebhookHandler)

	if adminHandler != nil {
		adminHandler.Register(mux)
	}

	return requestLogger(logger, mux)
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("http server listening", "addr", s.httpServer.Addr)
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		next.ServeHTTP(recorder, req)

		logger.Info("http request completed",
			"method", req.Method,
			"path", req.URL.Path,
			"status", recorder.status,
			"duration", time.Since(start),
		)
	})
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

