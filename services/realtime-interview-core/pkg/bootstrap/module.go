package bootstrap

import (
	"context"
	"net/http"
)

// Module defines lifecycle hooks each subsystem can implement.
type Module interface {
	Name() string
	RegisterRoutes(mux *http.ServeMux) error
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// BaseModule implements no-op lifecycle hooks for embedding.
type BaseModule struct{}

// Start is a no-op lifecycle hook.
func (BaseModule) Start(ctx context.Context) error { return nil }

// Shutdown is a no-op lifecycle hook.
func (BaseModule) Shutdown(ctx context.Context) error { return nil }
