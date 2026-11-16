package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const shutdownTimeout = 20 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "log-webhook")
	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	maxBody, err := envInt("MAX_BODY_BYTES", 10<<20)
	if err != nil {
		return err
	}
	if maxBody <= 0 {
		return errors.New("MAX_BODY_BYTES must be positive")
	}
	maxMessage, err := envInt("MAX_MESSAGE_BYTES", 16384)
	if err != nil {
		return err
	}
	port := envOr("PORT", "8080")
	endpoint := envOr("LOG_ENDPOINT", "/v1/logs")

	h := &LogHandler{
		Out:             os.Stdout,
		MaxBodyBytes:    int64(maxBody),
		MaxMessageBytes: maxMessage,
		FieldPrefix:     os.Getenv("FIELD_PREFIX"),
		Logger:          logger,
		Now:             time.Now,
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(endpoint, h),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	logger.Info("listening", "port", port, "endpoint", endpoint)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("stopped")
	return nil
}

func newMux(endpoint string, h http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(endpoint, h)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"healthy"}`)
	})
	return mux
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}
