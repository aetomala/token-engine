package observability

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ===== Service Metrics Interface =====

// Metrics is the service's metrics recording interface.
type Metrics interface {
	IncrementCounter(name string, labels map[string]string)
	AddCounter(name string, value float64, labels map[string]string)
	SetGauge(name string, value float64, labels map[string]string)
	RecordHistogram(name string, value float64, labels map[string]string)
	RecordDuration(name string, d time.Duration, labels map[string]string)
}

// ===== Metric Name Constants =====

const (
	MetricGRPCRequests     = "token_engine_grpc_requests_total"
	MetricGRPCDuration     = "token_engine_grpc_request_duration_seconds"
	MetricIdempotencyTotal = "token_engine_idempotency_total"
	MetricActiveTenants    = "token_engine_active_tenants"
	MetricRegistryOps      = "token_engine_tenant_registry_operations_total"
)

// ===== PrometheusMetrics =====

// PrometheusMetrics records metrics using Prometheus.
type PrometheusMetrics struct {
	counters   map[string]prometheus.Counter
	gauges     map[string]prometheus.Gauge
	histograms map[string]prometheus.Histogram
}

// NewPrometheusMetrics creates a new PrometheusMetrics and registers all metrics.
func NewPrometheusMetrics(reg *prometheus.Registry) *PrometheusMetrics {
	// ===== STEP 1: Create metric objects =====
	grpcReqs := prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricGRPCRequests,
		Help: "Total number of gRPC requests processed",
	})

	grpcDur := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: MetricGRPCDuration,
		Help: "Duration of gRPC requests in seconds",
	})

	idempotency := prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricIdempotencyTotal,
		Help: "Total number of idempotency operations",
	})

	activeTenants := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: MetricActiveTenants,
		Help: "Number of active tenants",
	})

	registryOps := prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricRegistryOps,
		Help: "Total number of tenant registry operations",
	})

	// ===== STEP 2: Register all metrics =====
	reg.MustRegister(grpcReqs, grpcDur, idempotency, activeTenants, registryOps)

	// ===== STEP 3: Build metric maps =====
	return &PrometheusMetrics{
		counters: map[string]prometheus.Counter{
			MetricGRPCRequests:     grpcReqs,
			MetricIdempotencyTotal: idempotency,
			MetricRegistryOps:      registryOps,
		},
		gauges: map[string]prometheus.Gauge{
			MetricActiveTenants: activeTenants,
		},
		histograms: map[string]prometheus.Histogram{
			MetricGRPCDuration: grpcDur,
		},
	}
}

// IncrementCounter increments a counter metric by 1.
func (p *PrometheusMetrics) IncrementCounter(name string, labels map[string]string) {
	counter, ok := p.counters[name]
	if !ok {
		panic(fmt.Sprintf("token-engine: unknown metric name %q", name))
	}
	counter.Inc()
}

// AddCounter adds a value to a counter metric.
func (p *PrometheusMetrics) AddCounter(name string, value float64, labels map[string]string) {
	counter, ok := p.counters[name]
	if !ok {
		panic(fmt.Sprintf("token-engine: unknown metric name %q", name))
	}
	counter.Add(value)
}

// SetGauge sets a gauge metric to a specific value.
func (p *PrometheusMetrics) SetGauge(name string, value float64, labels map[string]string) {
	gauge, ok := p.gauges[name]
	if !ok {
		panic(fmt.Sprintf("token-engine: unknown metric name %q", name))
	}
	gauge.Set(value)
}

// RecordHistogram records a value to a histogram metric.
func (p *PrometheusMetrics) RecordHistogram(name string, value float64, labels map[string]string) {
	histogram, ok := p.histograms[name]
	if !ok {
		panic(fmt.Sprintf("token-engine: unknown metric name %q", name))
	}
	histogram.Observe(value)
}

// RecordDuration records a duration to a histogram metric, converting to seconds.
func (p *PrometheusMetrics) RecordDuration(name string, d time.Duration, labels map[string]string) {
	histogram, ok := p.histograms[name]
	if !ok {
		panic(fmt.Sprintf("token-engine: unknown metric name %q", name))
	}
	histogram.Observe(d.Seconds())
}
