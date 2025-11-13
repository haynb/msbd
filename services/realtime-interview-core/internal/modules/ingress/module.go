package ingress

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"google.golang.org/grpc"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/internal/grpc/streaming"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
	realtimev1 "github.com/hayhandsome/msbd/services/realtime-interview-core/proto/realtime/v1"
)

// Module hosts the gRPC streaming ingress server.
type Module struct {
	bootstrap.BaseModule
	addr     string
	server   *grpc.Server
	listener net.Listener
	logger   *slog.Logger
}

// New constructs a new ingress module.
func New(cfg bootstrap.ServerConfig, guard *authn.Guard, gateway *speechgateway.Service, transcripts cloudEvents.Publisher, usageRecorder usage.Recorder, control realtimev1.ControlServiceServer, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	if gateway == nil {
		return nil
	}
	unary := []grpc.UnaryServerInterceptor{}
	stream := []grpc.StreamServerInterceptor{}
	if guard != nil {
		opts := authn.GRPCOptions{TokenFormat: crypto.TokenKindPASETO}
		unary = append(unary, guard.UnaryInterceptor(opts))
		stream = append(stream, guard.StreamInterceptor(opts))
	}
	grpcOpts := []grpc.ServerOption{}
	if len(unary) > 0 {
		grpcOpts = append(grpcOpts, grpc.ChainUnaryInterceptor(unary...))
	}
	if len(stream) > 0 {
		grpcOpts = append(grpcOpts, grpc.ChainStreamInterceptor(stream...))
	}
	server := grpc.NewServer(grpcOpts...)
	realtimev1.RegisterStreamingServiceServer(server, streaming.NewServer(gateway, transcripts, usageRecorder, logger))
	if control != nil {
		realtimev1.RegisterControlServiceServer(server, control)
	}
	return &Module{
		addr:   cfg.GRPCAddress,
		server: server,
		logger: logger.With("module", "ingress"),
	}
}

// Name implements bootstrap.Module.
func (m *Module) Name() string { return "ingress" }

// RegisterRoutes implements bootstrap.Module (no HTTP handlers yet).
func (m *Module) RegisterRoutes(mux *http.ServeMux) error { return nil }

// Start begins serving gRPC traffic.
func (m *Module) Start(ctx context.Context) error {
	if m == nil || m.server == nil {
		return errors.New("ingress module not configured")
	}
	if m.addr == "" {
		return errors.New("grpc address not configured")
	}
	ln, err := net.Listen("tcp", m.addr)
	if err != nil {
		return err
	}
	m.listener = ln
	go func() {
		m.logger.Info("gRPC ingress listening", "addr", m.addr)
		if err := m.server.Serve(ln); err != nil {
			m.logger.Error("grpc server stopped", "error", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the gRPC server.
func (m *Module) Shutdown(ctx context.Context) error {
	if m == nil || m.server == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		m.server.GracefulStop()
		if m.listener != nil {
			_ = m.listener.Close()
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		m.server.Stop()
		return ctx.Err()
	}
}
