package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// MetricSignerUp is a gauge that indicates if the signer service is up (always 1)
	MetricSignerUp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "signer_up",
			Help: "Whether the signer service is up (always 1)",
		},
	)

	// MetricSigningRequestsTotal counts signing requests by address and outcome
	MetricSigningRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signing_requests_total",
			Help: "Total number of transaction signing requests, by address and outcome",
		},
		[]string{"address", "status"},
	)

	// MetricSigningRequestDuration tracks the duration of signing requests
	MetricSigningRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "signing_request_duration_seconds",
			Help:    "Duration of signing requests in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"address", "request_type", "status"},
	)

	// MetricSigningRequestsInFlight tracks the number of signing requests currently being processed
	MetricSigningRequestsInFlight = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "signing_requests_in_flight",
			Help: "Number of signing requests currently being processed",
		},
		[]string{"address"},
	)

	// MetricConfiguredKeys is a gauge showing the number of configured signing keys
	MetricConfiguredKeys = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "signer_configured_keys",
			Help: "Number of configured signing keys",
		},
	)

	// MetricProviderType shows the provider type being used
	MetricProviderType = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "signer_provider_info",
			Help: "Information about the signer provider type (1 indicates the provider type in use)",
		},
		[]string{"provider_type"},
	)

	// MetricSigningErrorsTotal counts signing errors by type
	MetricSigningErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signing_errors_total",
			Help: "Total number of signing errors by error type",
		},
		[]string{"address", "error_type"},
	)

	// MetricRPCTotal counts RPC requests by method
	MetricRPCTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signer_rpc_requests_total",
			Help: "Total number of RPC requests by method and status",
		},
		[]string{"method", "status"},
	)

	MetricSignTransactionTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signer_signtransaction_total",
			Help: "Total number of transaction signing requests by client, status and error"},
		[]string{"client", "status", "error"},
	)

	MetricSignBlockPayloadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signer_signblockpayload_total",
			Help: "Total number of block payload signing requests by client, status and error"},
		[]string{"client", "status", "error"},
	)
)

// Timer helps track request duration
type Timer struct {
	start    time.Time
	address  string
	reqType  string
	status   string
	recorded bool
}

// NewTimer creates a new timer for tracking request duration
func NewTimer(address, reqType string) *Timer {
	return &Timer{
		start:   time.Now(),
		address: address,
		reqType: reqType,
	}
}

// RecordDuration records the duration and marks the status
func (t *Timer) RecordDuration(status string) {
	if t.recorded {
		return
	}
	t.status = status
	t.recorded = true
	MetricSigningRequestDuration.WithLabelValues(t.address, t.reqType, t.status).Observe(time.Since(t.start).Seconds())
}
