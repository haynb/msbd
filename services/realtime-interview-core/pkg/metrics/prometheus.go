package metrics

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector centralizes Prometheus instrumentation for the realtime service.
type Collector struct {
	registry prometheus.Registerer

	speechLatency   *prometheus.HistogramVec
	providerErrors  *prometheus.CounterVec
	circuitState    prometheus.Gauge
	circuitFailures prometheus.Gauge
	wsClients       prometheus.Gauge
	wsBackpressure  prometheus.Gauge
	usageBuffered   prometheus.Gauge
	usageFlushTotal *prometheus.CounterVec
	llmTokens       *prometheus.CounterVec
}

// NewCollector registers realtime metrics against the provided registry (defaulting to the global registry).
func NewCollector(reg prometheus.Registerer) *Collector {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	c := &Collector{registry: reg}

	// Ensure base Go/process collectors are registered once.
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	c.speechLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rtc_speech_segment_latency_ms",
		Help:    "Latency of speech provider segments (ms)",
		Buckets: []float64{10, 25, 50, 100, 250, 500, 1000, 2000, 4000},
	}, []string{"provider"})

	c.providerErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rtc_speech_provider_errors_total",
		Help: "Count of speech provider errors grouped by provider",
	}, []string{"provider"})

	c.circuitState = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rtc_speech_circuit_open",
		Help: "Circuit breaker state (1=open, 0=closed)",
	})

	c.circuitFailures = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rtc_speech_circuit_failures",
		Help: "Current failure count in the speech circuit breaker",
	})

	c.wsClients = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rtc_ws_clients",
		Help: "Number of connected WebSocket clients",
	})

	c.wsBackpressure = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rtc_ws_backpressure",
		Help: "Queued hub events waiting for delivery",
	})

	c.usageBuffered = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "rtc_usage_buffered_events",
		Help: "Number of usage events pending on-disk buffer",
	})

	c.usageFlushTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rtc_usage_flush_total",
		Help: "Total usage batches flushed with status",
	}, []string{"status"})

	c.llmTokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rtc_llm_tokens_total",
		Help: "LLM tokens consumed by orchestrator outputs",
	}, []string{"provider", "degraded"})

	reg.MustRegister(
		c.speechLatency,
		c.providerErrors,
		c.circuitState,
		c.circuitFailures,
		c.wsClients,
		c.wsBackpressure,
		c.usageBuffered,
		c.usageFlushTotal,
		c.llmTokens,
	)

	return c
}

// Handler exposes the HTTP handler for /metrics.
func (c *Collector) Handler() http.Handler {
	if c == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	gatherer := prometheus.DefaultGatherer
	if reg, ok := c.registry.(prometheus.Gatherer); ok {
		gatherer = reg
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
}

// ObserveSpeech records a speech latency sample.
func (c *Collector) ObserveSpeech(provider string, duration time.Duration) {
	if c == nil || duration <= 0 {
		return
	}
	c.speechLatency.WithLabelValues(label(provider)).Observe(float64(duration.Milliseconds()))
}

// RecordProviderError increments the provider error counter.
func (c *Collector) RecordProviderError(provider string) {
	if c == nil {
		return
	}
	c.providerErrors.WithLabelValues(label(provider)).Inc()
}

// SetSpeechCircuit updates breaker gauges.
func (c *Collector) SetSpeechCircuit(open bool, failures int) {
	if c == nil {
		return
	}
	if open {
		c.circuitState.Set(1)
	} else {
		c.circuitState.Set(0)
	}
	c.circuitFailures.Set(float64(failures))
}

// ObserveWSClients sets the websocket client gauge.
func (c *Collector) ObserveWSClients(total int) {
	if c == nil {
		return
	}
	if total < 0 {
		total = 0
	}
	c.wsClients.Set(float64(total))
}

// ObserveWSBackpressure updates the backlog gauge.
func (c *Collector) ObserveWSBackpressure(pending int) {
	if c == nil {
		return
	}
	if pending < 0 {
		pending = 0
	}
	c.wsBackpressure.Set(float64(pending))
}

// SetUsageBuffered updates buffered usage event gauge.
func (c *Collector) SetUsageBuffered(pending int64) {
	if c == nil {
		return
	}
	if pending < 0 {
		pending = 0
	}
	c.usageBuffered.Set(float64(pending))
}

// RecordUsageFlush increments flush status counters.
func (c *Collector) RecordUsageFlush(status string) {
	if c == nil {
		return
	}
	if strings.TrimSpace(status) == "" {
		status = "ok"
	}
	c.usageFlushTotal.WithLabelValues(status).Inc()
}

// ObserveLLMTokens tracks LLM token consumption.
func (c *Collector) ObserveLLMTokens(provider string, tokens int, degraded bool) {
	if c == nil || tokens <= 0 {
		return
	}
	c.llmTokens.WithLabelValues(label(provider), boolLabel(degraded)).Add(float64(tokens))
}

func label(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return v
}

func boolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
