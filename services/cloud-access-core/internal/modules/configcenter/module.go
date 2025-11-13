package configmodule

import (
	"net/http"

	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	"github.com/hayhandsome/msbd/services/cloud-access-core/internal/ws"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/bootstrap"
)

// Module wires the config center handlers into the HTTP server.
type Module struct {
	bootstrap.BaseModule
	handler *httpapi.ConfigHandler
	stream  *ws.ConfigStreamHandler
}

// New constructs a config module instance.
func New(handler *httpapi.ConfigHandler, stream *ws.ConfigStreamHandler) *Module {
	return &Module{handler: handler, stream: stream}
}

// Name implements bootstrap.Module.
func (m *Module) Name() string { return "config-center" }

// RegisterRoutes attaches the handlers to the mux.
func (m *Module) RegisterRoutes(mux *http.ServeMux) error {
	if mux == nil {
		return nil
	}
	if m.handler != nil {
		m.handler.Register(mux)
	}
	if m.stream != nil {
		mux.Handle("/configs/stream", m.stream)
	}
	return nil
}
