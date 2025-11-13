package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"nhooyr.io/websocket"
)

// ConfigStreamHandler upgrades HTTP connections to WebSockets for config updates.
type ConfigStreamHandler struct {
	auth       *iam.Service
	subscriber events.ConfigSubscriber
	logger     *slog.Logger
}

// NewConfigStreamHandler constructs a handler for config notifications.
func NewConfigStreamHandler(auth *iam.Service, subscriber events.ConfigSubscriber, logger *slog.Logger) *ConfigStreamHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigStreamHandler{
		auth:       auth,
		subscriber: subscriber,
		logger:     logger.With("component", "config_ws"),
	}
}

// ServeHTTP implements http.Handler.
func (h *ConfigStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, err := h.authorize(r)
	if err != nil {
		writeStreamError(w, err)
		return
	}
	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		writeStreamError(w, iam.ErrInvalidCredentials)
		return
	}
	ctx := r.Context()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		if h.logger != nil {
			h.logger.Warn("ws accept failed", "error", err)
		}
		return
	}
	defer conn.Close(websocket.StatusInternalError, "internal error")

	eventsCh, cancel, err := h.subscriber.Subscribe(ctx, tenantID)
	if err != nil {
		h.sendError(ctx, conn, err)
		conn.Close(websocket.StatusPolicyViolation, "subscribe error")
		return
	}
	defer cancel()

	ready := map[string]any{"type": "ready", "tenant_id": tenantID.String(), "session_id": claims.SessionID}
	if err := h.sendJSON(ctx, conn, ready); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "context canceled")
			return
		case evt, ok := <-eventsCh:
			if !ok {
				conn.Close(websocket.StatusNormalClosure, "stream closed")
				return
			}
			message := map[string]any{
				"type":         "config.update",
				"profile_name": evt.ProfileName,
				"version":      evt.Version,
				"timestamp":    evt.Timestamp,
				"metadata":     evt.Metadata,
			}
			if err := h.sendJSON(ctx, conn, message); err != nil {
				return
			}
		}
	}
}

func (h *ConfigStreamHandler) authorize(r *http.Request) (*crypto.Claims, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil, iam.ErrInvalidCredentials
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, iam.ErrInvalidCredentials
	}
	return h.auth.Validate(r.Context(), parts[1], crypto.TokenKindPASETO)
}

func (h *ConfigStreamHandler) sendJSON(ctx context.Context, conn *websocket.Conn, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		if !errors.Is(err, context.Canceled) && h.logger != nil {
			h.logger.Warn("ws write failed", "error", err)
		}
		return err
	}
	return nil
}

func (h *ConfigStreamHandler) sendError(ctx context.Context, conn *websocket.Conn, err error) {
	_ = h.sendJSON(ctx, conn, map[string]any{"type": "error", "message": err.Error()})
}

func writeStreamError(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	if !errors.Is(err, iam.ErrInvalidCredentials) {
		status = http.StatusInternalServerError
	}
	http.Error(w, err.Error(), status)
}
