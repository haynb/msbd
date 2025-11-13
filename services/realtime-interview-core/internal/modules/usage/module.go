package usage

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	usagepkg "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
)

// Module boots the usage aggregation background workers.
type Module struct {
	bootstrap.BaseModule
	svc      *usagepkg.Service
	exporter *usagepkg.Exporter
	logger   *slog.Logger
}

// New constructs a usage module instance.
func New(svc *usagepkg.Service, exporter *usagepkg.Exporter, logger *slog.Logger) *Module {
	if svc == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Module{svc: svc, exporter: exporter, logger: logger.With("module", "usage")}
}

// Name implements bootstrap.Module.
func (m *Module) Name() string { return "usage" }

// RegisterRoutes implements bootstrap.Module (no HTTP handlers yet).
func (m *Module) RegisterRoutes(mux *http.ServeMux) error { return nil }

// Start begins background flushers.
func (m *Module) Start(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if m.exporter != nil {
		if err := m.exporter.StartBuffer(ctx); err != nil {
			return err
		}
	}
	return m.svc.Start(ctx)
}

// Shutdown stops workers and flushes pending usage.
func (m *Module) Shutdown(ctx context.Context) error {
	if m == nil {
		return nil
	}
	var err error
	if m.svc != nil {
		err = m.svc.Shutdown(ctx)
	}
	if m.exporter != nil {
		_ = m.exporter.CloseBuffer()
	}
	return err
}
