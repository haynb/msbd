package gatewaymodule

import (
	"net/http"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/gateway"
)

// Module wires the gateway server into the HTTP mux.
type Module struct {
	bootstrap.BaseModule
	server *gateway.Server
}

// New constructs a gateway module.
func New(server *gateway.Server) *Module {
	return &Module{server: server}
}

// Name identifies the module.
func (m *Module) Name() string { return "gateway" }

// RegisterRoutes attaches gateway handlers.
func (m *Module) RegisterRoutes(mux *http.ServeMux) error {
	if m.server != nil {
		m.server.Register(mux)
	}
	return nil
}
