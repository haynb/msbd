package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/metrics"
)

// StreamBinding associates a Redis Stream with an outbound event kind.
type StreamBinding struct {
	Name string
	Kind string
}

// HubConfig configures the websocket hub.
type HubConfig struct {
	Streams        []StreamBinding
	BlockSeconds   int
	AllowAnonymous bool
}

// Hub upgrades HTTP requests to WebSockets and fan-outs stream events.
type Hub struct {
	redis    *redis.Client
	cfg      HubConfig
	logger   *slog.Logger
	metrics  *metrics.Collector
	register chan *client
	unreg    chan *client
	events   chan hubEvent
	clients  map[*client]struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	upgrader websocket.Upgrader
}

// NewHub constructs a hub instance.
func NewHub(redisClient *redis.Client, cfg HubConfig, metricsCollector *metrics.Collector, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	block := cfg.BlockSeconds
	if block <= 0 {
		block = 3
	}
	return &Hub{
		redis:    redisClient,
		cfg:      cfg,
		logger:   logger.With("component", "ws.hub"),
		metrics:  metricsCollector,
		register: make(chan *client, 32),
		unreg:    make(chan *client, 32),
		events:   make(chan hubEvent, 128),
		clients:  make(map[*client]struct{}),
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, HandshakeTimeout: 10 * time.Second},
	}
}

// Start launches the hub loops.
func (h *Hub) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	h.ctx, h.cancel = context.WithCancel(ctx)
	h.wg.Add(1)
	go h.run()
	if h.redis != nil && len(h.cfg.Streams) > 0 {
		h.wg.Add(1)
		go h.streamLoop()
	}
	return nil
}

// Shutdown stops background loops and closes connections.
func (h *Hub) Shutdown(ctx context.Context) error {
	if h.cancel != nil {
		h.cancel()
	}
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ServeHTTP upgrades clients to a websocket connection.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	tenantID := parseUUID(r.URL.Query().Get("tenant_id"))
	if claims, ok := authn.ClaimsFromContext(r.Context()); ok {
		if id := parseUUID(claims.TenantID); id != uuid.Nil {
			tenantID = id
		}
	}
	if tenantID == uuid.Nil && !h.cfg.AllowAnonymous {
		http.Error(w, "tenant scope required", http.StatusUnauthorized)
		return
	}
	sessionID := parseUUID(r.URL.Query().Get("session_id"))
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if !errors.Is(err, http.ErrHijacked) {
			h.logger.Warn("ws upgrade failed", "error", err)
		}
		return
	}
	client := &client{
		hub:     h,
		conn:    conn,
		tenant:  tenantID,
		session: sessionID,
		send:    make(chan []byte, 64),
	}
	select {
	case h.register <- client:
	case <-h.ctx.Done():
		conn.Close()
		return
	}
	client.start()
}

// Publish injects a manual event (used by tests).
func (h *Hub) Publish(evt hubEvent) {
	if evt.Payload == nil {
		return
	}
	select {
	case h.events <- evt:
	default:
		if h.logger != nil {
			h.logger.Warn("ws event dropped", "kind", evt.Kind)
		}
	}
	h.observeBackpressure()
}

func (h *Hub) run() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			for c := range h.clients {
				c.close()
			}
			h.observeClients()
			return
		case client := <-h.register:
			h.clients[client] = struct{}{}
			h.observeClients()
		case client := <-h.unreg:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.close()
			}
			h.observeClients()
		case evt := <-h.events:
			for c := range h.clients {
				if c.accepts(evt) {
					c.enqueue(evt)
				}
			}
			h.observeBackpressure()
		}
	}
}

func (h *Hub) streamLoop() {
	defer h.wg.Done()
	if h.redis == nil {
		return
	}
	lastIDs := make(map[string]string, len(h.cfg.Streams))
	for _, binding := range h.cfg.Streams {
		lastIDs[binding.Name] = "$"
	}
	block := time.Duration(h.cfg.BlockSeconds) * time.Second
	if block <= 0 {
		block = 3 * time.Second
	}
	for {
		select {
		case <-h.ctx.Done():
			return
		default:
		}
		streams := make([]string, 0, len(h.cfg.Streams)*2)
		for _, binding := range h.cfg.Streams {
			streams = append(streams, binding.Name, lastIDs[binding.Name])
		}
		res, err := h.redis.XRead(h.ctx, &redis.XReadArgs{Streams: streams, Count: 50, Block: block}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if h.ctx.Err() != nil {
				return
			}
			h.logger.Warn("ws stream read failed", "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, stream := range res {
			binding := h.binding(stream.Stream)
			if binding.Name == "" {
				continue
			}
			for _, msg := range stream.Messages {
				lastIDs[stream.Stream] = msg.ID
				evt, evtErr := h.eventFromMessage(binding, msg)
				if evtErr != nil {
					h.logger.Warn("ws stream decode failed", "error", evtErr, "stream", binding.Name)
					continue
				}
				h.Publish(evt)
			}
		}
	}
}

func (h *Hub) observeClients() {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.ObserveWSClients(len(h.clients))
}

func (h *Hub) observeBackpressure() {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.ObserveWSBackpressure(len(h.events))
}

func (h *Hub) binding(name string) StreamBinding {
	for _, binding := range h.cfg.Streams {
		if binding.Name == name {
			return binding
		}
	}
	return StreamBinding{}
}

func (h *Hub) eventFromMessage(binding StreamBinding, msg redis.XMessage) (hubEvent, error) {
	raw, ok := msg.Values["payload"]
	if !ok {
		return hubEvent{}, errors.New("missing payload")
	}
	bytes, err := toBytes(raw)
	if err != nil {
		return hubEvent{}, err
	}
	payload := map[string]any{}
	var envelope cloudEvents.Event
	if err := json.Unmarshal(bytes, &envelope); err == nil && envelope.Payload != nil {
		payload = envelope.Payload
	} else if err := json.Unmarshal(bytes, &payload); err != nil {
		return hubEvent{}, err
	}
	sessionID := parseUUID(fromMap(payload, "session_id"))
	tenantID := parseUUID(fromMap(payload, "tenant_id"))
	kind := binding.Kind
	if kind == "" {
		if action, _ := payload["action"].(string); action != "" {
			kind = action
		}
	}
	return hubEvent{Kind: kind, SessionID: sessionID, TenantID: tenantID, Payload: payload}, nil
}

func toBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return nil, errors.New("payload must be string or bytes")
	}
}

func fromMap(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

type hubEvent struct {
	Kind      string
	SessionID uuid.UUID
	TenantID  uuid.UUID
	Payload   map[string]any
}

type client struct {
	hub     *Hub
	conn    *websocket.Conn
	tenant  uuid.UUID
	session uuid.UUID
	send    chan []byte
}

func (c *client) start() {
	go c.writeLoop()
	ready := map[string]any{
		"type":       "ready",
		"tenant_id":  c.tenant.String(),
		"session_id": c.session.String(),
	}
	c.enqueuePayload(ready)
	c.readLoop()
}

func (c *client) accepts(evt hubEvent) bool {
	if evt.TenantID != uuid.Nil && c.tenant != uuid.Nil && evt.TenantID != c.tenant {
		return false
	}
	if c.session == uuid.Nil {
		return true
	}
	if evt.SessionID == uuid.Nil {
		return false
	}
	return evt.SessionID == c.session
}

func (c *client) enqueue(evt hubEvent) {
	message := map[string]any{
		"type":       evt.Kind,
		"session_id": evt.SessionID.String(),
		"tenant_id":  evt.TenantID.String(),
		"payload":    evt.Payload,
	}
	c.enqueuePayload(message)
}

func (c *client) enqueuePayload(payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	select {
	case c.send <- data:
	default:
		c.close()
	}
}

func (c *client) writeLoop() {
	for msg := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
	c.conn.Close()
}

func (c *client) readLoop() {
	c.conn.SetReadLimit(1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
	select {
	case c.hub.unreg <- c:
	case <-c.hub.ctx.Done():
	}
}

func (c *client) close() {
	defer func() { recover() }()
	close(c.send)
}

func parseUUID(value string) uuid.UUID {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}
	return id
}
