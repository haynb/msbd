package unit

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

func TestApplyRuntimeConfigUpdatesGateway(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gateway := speechgateway.NewService(bootstrap.SpeechGatewayConfig{Enabled: true}, "aliyun", nil, nil, nil, logger)
	adapter := &stubAliyunAdapter{}
	runtime := speechgateway.RuntimeConfig{
		DefaultProvider: "custom",
		Aliyun: &speechgateway.AliyunRuntimeConfig{
			AppKey:     "override",
			Format:     "opus",
			SampleRate: ptrInt(8000),
		},
	}
	base := bootstrap.ProvidersConfig{
		Default: "aliyun",
		Aliyun:  bootstrap.AliyunProviderConfig{AppKey: "base", Format: "pcm", SampleRate: 16000},
	}
	speechgateway.ApplyRuntimeConfig(runtime, base, gateway, speechgateway.ProviderAdapters{Aliyun: adapter}, logger)

	status := gateway.Status()
	require.Equal(t, "custom", status.DefaultProvider)
	require.Equal(t, "override", adapter.last.AppKey)
	require.Equal(t, "opus", adapter.last.Format)
	require.Equal(t, 8000, adapter.last.SampleRate)
}

func ptrInt(v int) *int {
	return &v
}

type stubAliyunAdapter struct {
	last bootstrap.AliyunProviderConfig
}

func (s *stubAliyunAdapter) UpdateConfig(cfg bootstrap.AliyunProviderConfig) error {
	s.last = cfg
	return nil
}
