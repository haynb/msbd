package iamproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	iamproto "github.com/hayhandsome/msbd/services/cloud-access-core/proto/iam"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Validator exposes token validation semantics shared by HTTP and gRPC interceptors.
type Validator interface {
	Validate(ctx context.Context, token string, kind crypto.TokenKind) (*crypto.Claims, error)
	Close() error
}

// Client validates tokens by calling cloud-access-core's AuthService.Validate RPC.
type Client struct {
	cfg    bootstrap.IAMClientConfig
	conn   *grpc.ClientConn
	stub   iamproto.AuthServiceClient
	logger *slog.Logger
}

// NewClient dials the IAM gRPC endpoint using the provided configuration.
func NewClient(ctx context.Context, cfg bootstrap.IAMClientConfig, logger *slog.Logger) (*Client, error) {
	if strings.TrimSpace(cfg.Address) == "" {
		return nil, errors.New("iam address required")
	}
	dialCtx, cancel := context.WithTimeout(ctx, cfg.Timeout())
	defer cancel()

	opts := []grpc.DialOption{}
	if cfg.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	conn, err := grpc.DialContext(dialCtx, cfg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial iam service: %w", err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		cfg:    cfg,
		conn:   conn,
		stub:   iamproto.NewAuthServiceClient(conn),
		logger: logger.With("component", "iamproxy"),
	}, nil
}

// Validate forwards validation to the upstream IAM service.
func (c *Client) Validate(ctx context.Context, token string, kind crypto.TokenKind) (*crypto.Claims, error) {
	if c == nil {
		return nil, errors.New("iam client not initialized")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("token required")
	}

	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout())
	defer cancel()

	req := &iamproto.ValidateRequest{Token: token, Format: toProtoFormat(kind)}
	resp, err := c.stub.Validate(callCtx, req)
	if err != nil {
		return nil, fmt.Errorf("iam validate: %w", err)
	}

	return &crypto.Claims{
		Subject:   resp.GetSubject(),
		TenantID:  resp.GetTenantId(),
		SessionID: resp.GetSessionId(),
		Roles:     resp.GetRoles(),
		Email:     resp.GetEmail(),
	}, nil
}

// Close shuts down the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func toProtoFormat(kind crypto.TokenKind) iamproto.TokenFormat {
	switch kind {
	case crypto.TokenKindPASETO:
		return iamproto.TokenFormat_TOKEN_FORMAT_PASETO
	default:
		return iamproto.TokenFormat_TOKEN_FORMAT_JWT
	}
}
