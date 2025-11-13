package policymodule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/google/uuid"
	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
	protov1 "github.com/hayhandsome/msbd/services/cloud-access-core/proto/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config contains module wiring parameters.
type Config struct {
	GRPCAddress string
}

// Module wires policy service into HTTP + gRPC endpoints.
type Module struct {
	bootstrap.BaseModule
	handler  *httpapi.PolicyHandler
	grpcAddr string
	grpc     *grpc.Server
	logger   *slog.Logger
	listener net.Listener
	once     sync.Once
}

// New builds the policy module instance.
func New(service *policy.Service, logger *slog.Logger, cfg Config, opts ...grpc.ServerOption) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	grpcServer := grpc.NewServer(opts...)
	protov1.RegisterPolicyServiceServer(grpcServer, &policyGRPCServer{service: service})
	return &Module{
		handler:  httpapi.NewPolicyHandler(service, logger),
		grpcAddr: cfg.GRPCAddress,
		grpc:     grpcServer,
		logger:   logger.With("module", "policy"),
	}
}

// Name identifies the module.
func (m *Module) Name() string { return "policy" }

// RegisterRoutes attaches HTTP handlers.
func (m *Module) RegisterRoutes(mux *http.ServeMux) error {
	if m.handler != nil {
		m.handler.Register(mux)
	}
	return nil
}

// Start boots the gRPC server.
func (m *Module) Start(ctx context.Context) error {
	if m.grpc == nil || m.grpcAddr == "" {
		return nil
	}
	var startErr error
	m.once.Do(func() {
		listener, err := net.Listen("tcp", m.grpcAddr)
		if err != nil {
			startErr = fmt.Errorf("policy grpc listen: %w", err)
			return
		}
		m.listener = listener
		go func() {
			m.logger.Info("Policy gRPC listening", "addr", m.grpcAddr)
			if err := m.grpc.Serve(listener); err != nil {
				m.logger.Error("policy gRPC stopped", "error", err)
			}
		}()
	})
	return startErr
}

// Shutdown stops the gRPC server.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.grpc == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		m.grpc.GracefulStop()
		if m.listener != nil {
			_ = m.listener.Close()
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		m.grpc.Stop()
		return ctx.Err()
	}
}

type policyGRPCServer struct {
	protov1.UnimplementedPolicyServiceServer
	service *policy.Service
}

func (s *policyGRPCServer) CheckAccess(ctx context.Context, req *protov1.CheckAccessRequest) (*protov1.CheckAccessResponse, error) {
	tenantID, err := parseUUID(req.GetTenantId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant_id")
	}
	userID, err := parseOptionalUUID(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id")
	}
	decision, err := s.service.CheckAccess(ctx, policy.CheckAccessRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    req.GetRoles(),
		Action:   req.GetAction(),
		Resource: req.GetResource(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &protov1.CheckAccessResponse{Allowed: decision.Allowed, Reason: decision.Reason, RuleName: decision.RuleName}, nil
}

func (s *policyGRPCServer) EvaluateQuota(ctx context.Context, req *protov1.EvaluateQuotaRequest) (*protov1.EvaluateQuotaResponse, error) {
	userID, err := parseUUID(req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id")
	}
	tenantID, err := parseOptionalUUID(req.GetTenantId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid tenant_id")
	}
	decision, err := s.service.EvaluateQuota(ctx, policy.QuotaRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    req.GetRoles(),
		Resource: req.GetResource(),
		Amount:   req.GetAmount(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &protov1.EvaluateQuotaResponse{Allowed: decision.Allowed, Reason: decision.Reason, State: decision.State, Remaining: decision.Remaining}, nil
}

func parseUUID(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}

func parseOptionalUUID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(value)
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, policy.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, policy.ErrTenantNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.Internal, "policy service error")
	}
}
