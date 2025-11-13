package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	realtimev1 "github.com/hayhandsome/msbd/services/realtime-interview-core/proto/realtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// kvMetadata collects repeated -meta flags into a map used for StreamInit metadata.
type kvMetadata map[string]string

func (m *kvMetadata) String() string {
	if m == nil || *m == nil {
		return ""
	}
	pairs := make([]string, 0, len(*m))
	for k, v := range *m {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, ",")
}

func (m *kvMetadata) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("metadata pair cannot be empty")
	}
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("metadata must be key=value, got %q", value)
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return fmt.Errorf("metadata key missing in %q", value)
	}
	if *m == nil {
		*m = make(map[string]string)
	}
	(*m)[key] = parts[1]
	return nil
}

func main() {
	var (
		grpcAddr   = flag.String("grpc", "localhost:9084", "gRPC host:port for StreamingService")
		httpBase   = flag.String("http", "http://localhost:8084", "HTTP base used to derive ws:// URL when -ws-url is unset")
		wsURL      = flag.String("ws-url", "", "Optional full WebSocket URL (defaults to http base + /ws/realtime)")
		enableWS   = flag.Bool("ws", false, "Subscribe to /ws/realtime for the session")
		wsDuration = flag.Duration("ws-duration", 10*time.Second, "How long to read from the WebSocket feed")
		sessionID  = flag.String("session", "", "Session ID (auto-generated when empty)")
		tenantID   = flag.String("tenant", "", "Tenant ID for WebSocket filtering")
		token      = flag.String("token", "", "Bearer token from cloud-access-core (required)")
		audioPath  = flag.String("audio", "", "Raw PCM file to send")
		provider   = flag.String("provider", "aliyun", "Speech provider key registered with the gateway")
		sampleRate = flag.Int("sample-rate", 16000, "Audio sample rate (Hz)")
		format     = flag.String("format", "pcm", "Audio format label")
		chunkBytes = flag.Int("chunk-bytes", 32000, "Chunk size for audio payloads")
	)
	metadataMap := kvMetadata{}
	flag.Var(&metadataMap, "meta", "Attach metadata key=value to the StreamInit payload (repeatable)")
	flag.Parse()

	if *token == "" {
		log.Fatal("-token / RTC_BEARER is required")
	}
	if *audioPath == "" {
		log.Fatal("-audio path is required")
	}
	data, err := os.ReadFile(*audioPath)
	if err != nil {
		log.Fatalf("read audio: %v", err)
	}
	if len(data) == 0 {
		log.Fatal("audio file is empty")
	}
	if *chunkBytes <= 0 {
		log.Fatalf("chunk-bytes must be positive (got %d)", *chunkBytes)
	}

	if *sessionID == "" {
		id := uuid.NewString()
		sessionID = &id
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var wsWG sync.WaitGroup
	if *enableWS {
		fullURL, err := buildWebsocketURL(*httpBase, *wsURL, *sessionID, *tenantID)
		if err != nil {
			log.Fatalf("derive websocket url: %v", err)
		}
		wsWG.Add(1)
		go func() {
			defer wsWG.Done()
			watchWebsocket(ctx, fullURL, *token, *wsDuration)
		}()
	}

	conn, err := grpc.DialContext(ctx, *grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial gRPC: %v", err)
	}
	defer conn.Close()

	client := realtimev1.NewStreamingServiceClient(conn)
	streamCtx := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", fmt.Sprintf("Bearer %s", *token)))
	stream, err := client.StreamAudio(streamCtx)
	if err != nil {
		log.Fatalf("start stream: %v", err)
	}

	initMetadata := map[string]string{}
	for k, v := range metadataMap {
		initMetadata[k] = v
	}

	if err := stream.Send(&realtimev1.StreamAudioRequest{
		Payload: &realtimev1.StreamAudioRequest_Init{
			Init: &realtimev1.StreamInit{
				SessionId:  *sessionID,
				Provider:   *provider,
				SampleRate: int32(*sampleRate),
				Format:     *format,
				Metadata:   initMetadata,
			},
		},
	}); err != nil {
		log.Fatalf("send init: %v", err)
	}

	recvDone := make(chan error, 1)
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				recvDone <- nil
				return
			}
			if err != nil {
				recvDone <- err
				return
			}
			switch payload := resp.Payload.(type) {
			case *realtimev1.StreamAudioResponse_Ack:
				log.Printf("[ack] session=%s provider=%s sample_rate=%d format=%s", payload.Ack.SessionId, payload.Ack.Provider, payload.Ack.SampleRate, payload.Ack.Format)
			case *realtimev1.StreamAudioResponse_Transcript:
				log.Printf("[transcript] seq=%d final=%t text=%q conf=%.3f", payload.Transcript.Sequence, payload.Transcript.IsFinal, payload.Transcript.Text, payload.Transcript.Confidence)
			case *realtimev1.StreamAudioResponse_Error:
				log.Printf("[error] code=%s message=%s", payload.Error.Code, payload.Error.Message)
			default:
				log.Printf("[debug] received %T", resp.Payload)
			}
		}
	}()

	sequence := int64(1)
	for offset := 0; offset < len(data); offset += *chunkBytes {
		end := offset + *chunkBytes
		if end > len(data) {
			end = len(data)
		}
		chunk := &realtimev1.AudioChunk{Data: data[offset:end], Sequence: sequence, IsLast: end == len(data)}
		if err := stream.Send(&realtimev1.StreamAudioRequest{Payload: &realtimev1.StreamAudioRequest_Chunk{Chunk: chunk}}); err != nil {
			log.Fatalf("send chunk %d: %v", sequence, err)
		}
		sequence++
	}
	if err := stream.CloseSend(); err != nil {
		log.Printf("close send: %v", err)
	}

	if err := <-recvDone; err != nil {
		log.Printf("stream ended with error: %v", err)
	}
	wsWG.Wait()
}

func buildWebsocketURL(httpBase, override, sessionID, tenantID string) (string, error) {
	base := strings.TrimSpace(override)
	if base == "" {
		if strings.TrimSpace(httpBase) == "" {
			return "", errors.New("http base url required")
		}
		u, err := url.Parse(httpBase)
		if err != nil {
			return "", err
		}
		switch strings.ToLower(u.Scheme) {
		case "https":
			u.Scheme = "wss"
		default:
			u.Scheme = "ws"
		}
		u.Path = strings.TrimSuffix(u.Path, "/") + "/ws/realtime"
		base = u.String()
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if strings.TrimSpace(sessionID) != "" {
		q.Set("session_id", sessionID)
	}
	if strings.TrimSpace(tenantID) != "" {
		q.Set("tenant_id", tenantID)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func watchWebsocket(ctx context.Context, wsURL, token string, duration time.Duration) {
	if strings.TrimSpace(wsURL) == "" {
		log.Println("[ws] url not provided; skipping")
		return
	}
	header := http.Header{}
	if strings.TrimSpace(token) != "" {
		header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		log.Printf("[ws] dial: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("[ws] connected to %s", wsURL)
	deadline := time.NewTimer(duration)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		select {
		case <-ctx.Done():
			log.Println("[ws] context canceled")
			return
		case <-deadline.C:
			log.Println("[ws] done (duration elapsed)")
			return
		default:
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) || errors.Is(err, io.EOF) {
				log.Println("[ws] closed by server")
				return
			}
			log.Printf("[ws] read: %v", err)
			return
		}
		log.Printf("[ws] %s", string(msg))
	}
}
