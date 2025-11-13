package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// App wires config, logger, router, and registered modules into a runnable service.
type App struct {
	cfg     *Config
	logger  *slog.Logger
	mux     *http.ServeMux
	modules []Module
	counter atomic.Uint64
}

// NewApp builds a new application scaffold with lightweight middleware.
func NewApp(cfg *Config, logger *slog.Logger) *App {
	mux := http.NewServeMux()
	// default heartbeat endpoint
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	return &App{
		cfg:     cfg,
		logger:  logger.With("component", "app"),
		mux:     mux,
		modules: make([]Module, 0),
	}
}

// RegisterModules registers modules and gives them a chance to attach routes.
func (a *App) RegisterModules(mods ...Module) error {
	for _, mod := range mods {
		if err := mod.RegisterRoutes(a.mux); err != nil {
			return fmt.Errorf("register module %s: %w", mod.Name(), err)
		}
		a.modules = append(a.modules, mod)
		a.logger.Info("module registered", "module", mod.Name())
	}
	return nil
}

// Router exposes the mux for tests.
func (a *App) Router() *http.ServeMux {
	return a.mux
}

// Run starts HTTP serving and handles graceful shutdown.
func (a *App) Run(ctx context.Context) error {
	for _, mod := range a.modules {
		if err := mod.Start(ctx); err != nil {
			return fmt.Errorf("start module %s: %w", mod.Name(), err)
		}
	}

	handler := a.wrapHandler(a.mux)
	server := &http.Server{
		Addr:              a.cfg.Server.Address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("HTTP server listening", "addr", a.cfg.Server.Address)
		if err := server.ListenAndServe(); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout())
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	for i := len(a.modules) - 1; i >= 0; i-- {
		if err := a.modules[i].Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("stop module %s: %w", a.modules[i].Name(), err)
		}
	}

	a.logger.Info("shutdown complete")
	return nil
}

func (a *App) wrapHandler(next http.Handler) http.Handler {
	handler := requestIDMiddleware(next, &a.counter)
	handler = loggingMiddleware(a.logger, handler)
	handler = recoverMiddleware(a.logger, handler)
	handler = timeoutMiddleware(handler, 60*time.Second)
	return handler
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func requestIDMiddleware(next http.Handler, counter *atomic.Uint64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID(counter)
			r.Header.Set("X-Request-ID", requestID)
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		logger.Info("request complete",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", getRequestID(r.Context()),
		)
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered", "error", rec, "request_id", getRequestID(r.Context()))
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func timeoutMiddleware(next http.Handler, d time.Duration) http.Handler {
	return http.TimeoutHandler(next, d, "request timed out")
}

type requestIDKey struct{}

func getRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

func generateRequestID(counter *atomic.Uint64) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", time.Now().UTC().Format(time.RFC3339Nano), counter.Add(1))
	}
	return fmt.Sprintf("%s-%s-%d",
		time.Now().UTC().Format("20060102T150405Z"),
		hex.EncodeToString(buf[:6]),
		counter.Add(1),
	)
}
