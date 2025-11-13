package bootstrap

import (
	"encoding/json"
	"net/http"
	"time"
)

// NewHealthModule registers liveness/readiness endpoints.
func NewHealthModule() Module {
	return &healthModule{}
}

type healthModule struct {
	BaseModule
}

func (m *healthModule) Name() string {
	return "health"
}

func (m *healthModule) RegisterRoutes(mux *http.ServeMux) error {
	mux.Handle("/healthz", http.HandlerFunc(m.handleHealth))
	mux.Handle("/readyz", http.HandlerFunc(m.handleReady))
	return nil
}

func (m *healthModule) handleHealth(w http.ResponseWriter, r *http.Request) {
	m.writeJSON(w, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

func (m *healthModule) handleReady(w http.ResponseWriter, r *http.Request) {
	m.writeJSON(w, map[string]any{
		"status":  "ready",
		"version": "0.1.0",
	})
}

func (m *healthModule) writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
