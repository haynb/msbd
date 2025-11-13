package sessionmodule

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	httpapi "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/http"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/internal/ws"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

// Config wires API + WebSocket paths/streams.
type Config struct {
	APIPrefix     string
	WebsocketPath string
}

// Module registers session REST + WS surfaces.
type Module struct {
	bootstrap.BaseModule
	cfg      Config
	guard    *authn.Guard
	handler  *httpapi.SessionsHandler
	provider *httpapi.ProviderHandler
	hub      *ws.Hub
	logger   *slog.Logger
}

// New constructs the module with required dependencies.
func New(cfg Config, guard *authn.Guard, service *sessions.Service, gateway *speechgateway.Service, runtimeStore *speechgateway.ConfigStore, hub *ws.Hub, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.APIPrefix) == "" {
		cfg.APIPrefix = "/api/realtime"
	}
	if strings.TrimSpace(cfg.WebsocketPath) == "" {
		cfg.WebsocketPath = "/ws/realtime"
	}
	return &Module{
		cfg:      cfg,
		guard:    guard,
		handler:  httpapi.NewSessionsHandler(service, logger),
		provider: httpapi.NewProviderHandler(gateway, runtimeStore, logger),
		hub:      hub,
		logger:   logger.With("module", "sessions"),
	}
}

// Name implements bootstrap.Module.
func (m *Module) Name() string { return "sessions" }

// RegisterRoutes registers REST + WS handlers.
func (m *Module) RegisterRoutes(mux *http.ServeMux) error {
	router := chi.NewRouter()
	if m.handler != nil {
		router.Group(func(r chi.Router) {
			if m.guard != nil {
				r.Use(m.guard.HTTPMiddleware(authn.HTTPOptions{TokenFormat: crypto.TokenKindJWT, RequiredRoles: []string{"interviewer", "ops", "admin"}}))
			}
			m.handler.Register(r)
		})
	}
	if m.provider != nil {
		router.Group(func(r chi.Router) {
			if m.guard != nil {
				r.Use(m.guard.HTTPMiddleware(authn.HTTPOptions{TokenFormat: crypto.TokenKindJWT, RequiredRoles: []string{"ops", "admin"}}))
			}
			m.provider.Register(r)
		})
	}
	prefix := strings.TrimSuffix(m.cfg.APIPrefix, "/")
	if prefix == "" {
		prefix = "/api/realtime"
	}
	handler := http.StripPrefix(prefix, router)
	mux.Handle(prefix, handler)
	mux.Handle(prefix+"/", handler)
	if m.hub != nil {
		wsHandler := http.Handler(m.hub)
		if m.guard != nil {
			wsHandler = m.guard.HTTPMiddleware(authn.HTTPOptions{TokenFormat: crypto.TokenKindJWT})(wsHandler)
		}
		mux.Handle(m.cfg.WebsocketPath, wsHandler)
	}
	return nil
}

// Start boots the websocket hub.
func (m *Module) Start(ctx context.Context) error {
	if m.hub != nil {
		return m.hub.Start(ctx)
	}
	return nil
}

// Shutdown stops background workers.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.hub != nil {
		return m.hub.Shutdown(ctx)
	}
	return nil
}
