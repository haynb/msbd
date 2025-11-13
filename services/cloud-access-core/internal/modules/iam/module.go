package iammodule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	pkgiam "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	proto "github.com/hayhandsome/msbd/services/cloud-access-core/proto/iam"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Config configures auxiliary IAM module behaviour.
type Config struct {
	GRPCAddress string
}

// Module wires IAM service into HTTP + gRPC endpoints.
type Module struct {
	bootstrap.BaseModule
	handler       *httpapi.AuthHandler
	deviceHandler *httpapi.DeviceHandler
	grpcAddr      string
	grpc          *grpc.Server
	listener      net.Listener
	logger        *slog.Logger
	startOnce     sync.Once
	consumer      *events.HeartbeatConsumer
}

// New constructs an IAM module instance.
func New(authSvc *pkgiam.Service, deviceSvc *pkgiam.DeviceService, consumer *events.HeartbeatConsumer, logger *slog.Logger, cfg Config, opts ...grpc.ServerOption) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	grpcServer := grpc.NewServer(opts...)
	proto.RegisterAuthServiceServer(grpcServer, newAuthGRPCServer(authSvc))
	proto.RegisterDeviceServiceServer(grpcServer, newDeviceGRPCServer(authSvc, deviceSvc))
	return &Module{
		handler:       httpapi.NewAuthHandler(authSvc, logger),
		deviceHandler: httpapi.NewDeviceHandler(authSvc, deviceSvc, logger),
		grpcAddr:      cfg.GRPCAddress,
		grpc:          grpcServer,
		logger:        logger.With("module", "iam"),
		consumer:      consumer,
	}
}

// Name implements bootstrap.Module.
func (m *Module) Name() string { return "iam" }

// RegisterRoutes registers REST handlers.
func (m *Module) RegisterRoutes(mux *http.ServeMux) error {
	if m.handler != nil {
		m.handler.Register(mux)
	}
	if m.deviceHandler != nil {
		m.deviceHandler.Register(mux)
	}
	return nil
}

// Start begins serving gRPC traffic.
func (m *Module) Start(ctx context.Context) error {
	if m.grpcAddr == "" {
		return nil
	}
	var startErr error
	m.startOnce.Do(func() {
		listener, err := net.Listen("tcp", m.grpcAddr)
		if err != nil {
			startErr = fmt.Errorf("iam grpc listen: %w", err)
			return
		}
		m.listener = listener
		go func() {
			m.logger.Info("IAM gRPC listening", "addr", m.grpcAddr)
			if err := m.grpc.Serve(listener); err != nil {
				m.logger.Error("iam gRPC server stopped", "error", err)
			}
		}()
	})
	if startErr != nil {
		return startErr
	}
	if m.consumer != nil {
		if err := m.consumer.Start(ctx); err != nil {
			return fmt.Errorf("start heartbeat consumer: %w", err)
		}
	}
	return startErr
}

// Shutdown gracefully stops the gRPC server.
func (m *Module) Shutdown(ctx context.Context) error {
	if m.grpc == nil {
		if m.consumer != nil {
			return m.consumer.Shutdown(ctx)
		}
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
	case <-ctx.Done():
		m.grpc.Stop()
		return ctx.Err()
	}
	if m.consumer != nil {
		return m.consumer.Shutdown(ctx)
	}
	return nil
}

type authGRPCServer struct {
	proto.UnimplementedAuthServiceServer
	service *pkgiam.Service
}

func newAuthGRPCServer(service *pkgiam.Service) proto.AuthServiceServer {
	return &authGRPCServer{service: service}
}

func (s *authGRPCServer) Login(ctx context.Context, req *proto.LoginRequest) (*proto.TokenResponse, error) {
	result, err := s.service.Login(ctx, pkgiam.LoginRequest{
		Email:             req.GetEmail(),
		Password:          req.GetPassword(),
		ClientKind:        req.GetClientKind(),
		DeviceFingerprint: req.GetDeviceFingerprint(),
		ClientIP:          req.GetClientIp(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return toProtoResponse(result), nil
}

func (s *authGRPCServer) Refresh(ctx context.Context, req *proto.RefreshRequest) (*proto.TokenResponse, error) {
	result, err := s.service.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, toStatus(err)
	}
	return toProtoResponse(result), nil
}

func (s *authGRPCServer) Logout(ctx context.Context, req *proto.LogoutRequest) (*proto.LogoutResponse, error) {
	if err := s.service.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, toStatus(err)
	}
	return &proto.LogoutResponse{Revoked: true}, nil
}

func (s *authGRPCServer) Validate(ctx context.Context, req *proto.ValidateRequest) (*proto.ValidateResponse, error) {
	claims, err := s.service.Validate(ctx, req.GetToken(), toTokenKind(req.GetFormat()))
	if err != nil {
		return nil, toStatus(err)
	}
	return &proto.ValidateResponse{
		Subject:   claims.Subject,
		TenantId:  claims.TenantID,
		SessionId: claims.SessionID,
		Roles:     claims.Roles,
		Email:     claims.Email,
	}, nil
}

func toProtoResponse(result *pkgiam.AuthResult) *proto.TokenResponse {
	now := time.Now().UTC()
	accessDur := result.AccessExpiresAt.Sub(now)
	refreshDur := result.RefreshExpiresAt.Sub(now)
	accessIn := int64(accessDur.Seconds())
	refreshIn := int64(refreshDur.Seconds())
	if accessIn < 0 {
		accessIn = 0
	}
	if refreshIn < 0 {
		refreshIn = 0
	}
	return &proto.TokenResponse{
		AccessToken:      result.AccessToken,
		DeviceToken:      result.DeviceToken,
		RefreshToken:     result.RefreshToken,
		AccessExpiresIn:  accessIn,
		RefreshExpiresIn: refreshIn,
		SessionId:        result.SessionID,
		TenantId:         result.TenantID.String(),
		UserId:           result.UserID.String(),
	}
}

func toTokenKind(format proto.TokenFormat) crypto.TokenKind {
	switch format {
	case proto.TokenFormat_TOKEN_FORMAT_PASETO:
		return crypto.TokenKindPASETO
	default:
		return crypto.TokenKindJWT
	}
}

type deviceGRPCServer struct {
	proto.UnimplementedDeviceServiceServer
	authSvc   *pkgiam.Service
	deviceSvc *pkgiam.DeviceService
}

func newDeviceGRPCServer(auth *pkgiam.Service, device *pkgiam.DeviceService) proto.DeviceServiceServer {
	return &deviceGRPCServer{authSvc: auth, deviceSvc: device}
}

func (s *deviceGRPCServer) RequestPairing(ctx context.Context, req *proto.DevicePairingRequest) (*proto.DevicePairingResponse, error) {
	result, err := s.deviceSvc.RequestPairing(ctx, pkgiam.PairingRequest{
		Platform:      req.GetPlatform(),
		Fingerprint:   req.GetFingerprint(),
		ClientVersion: req.GetClientVersion(),
		DisplayName:   req.GetDisplayName(),
		Metadata:      mapStringToAny(req.GetMetadata()),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	expiresIn := time.Until(result.ExpiresAt).Seconds()
	if expiresIn < 0 {
		expiresIn = 0
	}
	return &proto.DevicePairingResponse{
		Code:      result.Code,
		Token:     result.Token,
		ExpiresIn: int64(expiresIn),
	}, nil
}

func (s *deviceGRPCServer) ApprovePairing(ctx context.Context, req *proto.ApprovePairingRequest) (*proto.ApprovePairingResponse, error) {
	identity, err := s.identityFromMetadata(ctx, crypto.TokenKindJWT)
	if err != nil {
		return nil, toStatus(err)
	}
	result, err := s.deviceSvc.ApprovePairing(ctx, identity, pkgiam.ApprovePairingRequest{
		Code:        req.GetCode(),
		ProfileName: req.GetProfileName(),
		TrustScore:  int(req.GetTrustScore()),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &proto.ApprovePairingResponse{
		DeviceId:      result.DeviceID.String(),
		Status:        result.Status,
		ConfigVersion: int32(result.ConfigVersion),
	}, nil
}

func (s *deviceGRPCServer) ClaimPairing(ctx context.Context, req *proto.ClaimPairingRequest) (*proto.ClaimPairingResponse, error) {
	result, err := s.deviceSvc.ClaimPairing(ctx, pkgiam.ClaimPairingRequest{
		Code:  req.GetCode(),
		Token: req.GetToken(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &proto.ClaimPairingResponse{
		DeviceId:         result.DeviceID.String(),
		ConfigVersion:    int32(result.ConfigVersion),
		EncryptedProfile: result.EncryptedProfile,
	}, nil
}

func (s *deviceGRPCServer) SendHeartbeat(ctx context.Context, req *proto.DeviceHeartbeatRequest) (*proto.DeviceHeartbeatResponse, error) {
	identity, err := s.identityFromMetadata(ctx, crypto.TokenKindPASETO)
	if err != nil {
		return nil, toStatus(err)
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return nil, toStatus(pkgiam.ErrInvalidHeartbeatStatus)
	}
	if err := s.deviceSvc.ReportHeartbeat(ctx, identity, pkgiam.HeartbeatRequest{
		DeviceID: deviceID,
		Status:   req.GetStatus(),
		Metrics:  mapStringToAny(req.GetMetrics()),
	}); err != nil {
		return nil, toStatus(err)
	}
	return &proto.DeviceHeartbeatResponse{Accepted: true}, nil
}

func (s *deviceGRPCServer) RevokeDevice(ctx context.Context, req *proto.RevokeDeviceRequest) (*proto.RevokeDeviceResponse, error) {
	identity, err := s.identityFromMetadata(ctx, crypto.TokenKindJWT)
	if err != nil {
		return nil, toStatus(err)
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return nil, toStatus(pkgiam.ErrDeviceNotFound)
	}
	if err := s.deviceSvc.RevokeDevice(ctx, identity, pkgiam.RevokeDeviceRequest{
		DeviceID: deviceID,
		Reason:   req.GetReason(),
	}); err != nil {
		return nil, toStatus(err)
	}
	return &proto.RevokeDeviceResponse{Revoked: true}, nil
}

func (s *deviceGRPCServer) identityFromMetadata(ctx context.Context, kind crypto.TokenKind) (pkgiam.DeviceIdentity, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return pkgiam.DeviceIdentity{}, pkgiam.ErrInvalidCredentials
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return pkgiam.DeviceIdentity{}, pkgiam.ErrInvalidCredentials
	}
	token := values[0]
	parts := strings.SplitN(token, " ", 2)
	if len(parts) == 2 {
		token = parts[1]
	}
	claims, err := s.authSvc.Validate(ctx, token, kind)
	if err != nil {
		return pkgiam.DeviceIdentity{}, err
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return pkgiam.DeviceIdentity{}, pkgiam.ErrInvalidCredentials
	}
	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		return pkgiam.DeviceIdentity{}, pkgiam.ErrInvalidCredentials
	}
	return pkgiam.DeviceIdentity{
		UserID:   userID,
		TenantID: tenantID,
		Email:    claims.Email,
	}, nil
}

func mapStringToAny(src map[string]string) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func toStatus(err error) error {
	switch {
	case errors.Is(err, pkgiam.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, pkgiam.ErrAccountFrozen):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, pkgiam.ErrSessionExpired):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, pkgiam.ErrSessionNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, pkgiam.ErrPairingNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, pkgiam.ErrPairingExpired):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, pkgiam.ErrPairingPending):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, pkgiam.ErrPairingTokenMismatch):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, pkgiam.ErrDeviceNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, pkgiam.ErrInvalidHeartbeatStatus):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, pkgiam.ErrPairingCodeConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
