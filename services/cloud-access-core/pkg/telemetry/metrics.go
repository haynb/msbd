package telemetry

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics wraps the Prometheus registry and relevant counters.
type Metrics struct {
	registry      *prometheus.Registry
	handler       http.Handler
	auditsTotal   *prometheus.CounterVec
	auditFailures *prometheus.CounterVec
	auditLatency  *prometheus.HistogramVec
	bufferBacklog prometheus.Gauge
}

// NewCollector instantiates the telemetry metrics registry.
func NewCollector() *Metrics {
	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	registry.MustRegister(prometheus.NewGoCollector())

	auditsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cloud_access_audit_events_total",
		Help: "Total number of audit events recorded",
	}, []string{"action", "result"})

	auditFailures := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cloud_access_audit_failures_total",
		Help: "Number of audit pipeline failures grouped by stage",
	}, []string{"stage"})

	auditLatency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cloud_access_audit_latency_ms",
		Help:    "Observed latency (ms) of audited actions",
		Buckets: []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2000},
	}, []string{"action"})

	bufferBacklog := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cloud_access_audit_buffer_backlog",
		Help: "Number of events waiting in the local telemetry buffer",
	})

	registry.MustRegister(auditsTotal, auditFailures, auditLatency, bufferBacklog)

	return &Metrics{
		registry:      registry,
		handler:       promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		auditsTotal:   auditsTotal,
		auditFailures: auditFailures,
		auditLatency:  auditLatency,
		bufferBacklog: bufferBacklog,
	}
}

// Handler exposes the HTTP handler for /metrics.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return m.handler
}

// ObserveAudit records audit success latency + counts.
func (m *Metrics) ObserveAudit(action, result string, latency time.Duration) {
	if m == nil {
		return
	}
	elapsed := float64(latency.Milliseconds())
	m.auditsTotal.WithLabelValues(action, result).Inc()
	if elapsed < 0 {
		elapsed = 0
	}
	m.auditLatency.WithLabelValues(action).Observe(elapsed)
}

// RecordAuditFailure increments the failure counter for the stage.
func (m *Metrics) RecordAuditFailure(stage string) {
	if m == nil {
		return
	}
	if stage == "" {
		stage = "unknown"
	}
	m.auditFailures.WithLabelValues(stage).Inc()
}

// SetBufferedEvents updates the backlog gauge.
func (m *Metrics) SetBufferedEvents(backlog int64) {
	if m == nil {
		return
	}
	if backlog < 0 {
		backlog = 0
	}
	m.bufferBacklog.Set(float64(backlog))
}
