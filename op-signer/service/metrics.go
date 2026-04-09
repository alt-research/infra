package service

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// KeyMetricInfo contains the information needed to initialize/delete metrics for a key
type KeyMetricInfo struct {
	Address  string
	ClientCN string
}

type signingRequestMetricLabelSet struct {
	address  string
	clientCN string
}

type signingRequestMetricsState struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inFlight map[signingRequestMetricLabelSet]int
}

type deletedSigningRequestMetric struct {
	expiresAt time.Time
}

const deletedSigningRequestMetricTTL = time.Hour

func newSigningRequestMetricsState() *signingRequestMetricsState {
	state := &signingRequestMetricsState{
		inFlight: make(map[signingRequestMetricLabelSet]int),
	}
	state.cond = sync.NewCond(&state.mu)
	return state
}

var (
	signingRequestMetricsTracker = newSigningRequestMetricsState()
	deletedSigningRequestMetrics sync.Map

	// MetricSignerUp is a gauge that indicates if the signer service is up (always 1)
	MetricSignerUp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "signer_up",
			Help: "Whether the signer service is up (always 1)",
		},
	)

	// MetricSigningRequestsTotal counts signing requests by address, client CN and outcome
	MetricSigningRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "signing_requests_total",
			Help: "Total number of transaction signing requests, by address, client CN and outcome",
		},
		[]string{"address", "client_cn", "status"},
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

// InitKeyMetrics initializes placeholder request counters for a CN-restricted key.
func InitKeyMetrics(address, clientCN string) {
	if address == "" || clientCN == "" {
		return
	}

	deletedSigningRequestMetrics.Delete(signingRequestMetricLabelSet{address: address, clientCN: clientCN})
	pruneExpiredDeletedSigningRequestMetrics()
	MetricSigningRequestsTotal.WithLabelValues(address, clientCN, "success")
	MetricSigningRequestsTotal.WithLabelValues(address, clientCN, "error")
}

// InitAllKeyMetrics initializes placeholder metrics for all configured keys
func InitAllKeyMetrics(keys []KeyMetricInfo) {
	for _, key := range keys {
		InitKeyMetrics(key.Address, key.ClientCN)
	}
}

// DeleteKeyMetrics removes request counters for a specific address/clientCN combination.
// This should be called when a key is removed or when its clientCN is changed.
func DeleteKeyMetrics(address, clientCN string) {
	if address == "" || clientCN == "" {
		return
	}

	labelSet := signingRequestMetricLabelSet{address: address, clientCN: clientCN}
	deletedSigningRequestMetrics.Store(labelSet, deletedSigningRequestMetric{expiresAt: time.Now().Add(deletedSigningRequestMetricTTL)})
	pruneExpiredDeletedSigningRequestMetrics()
	waitForSigningRequestLabelSetIdle(address, clientCN)
	MetricSigningRequestsTotal.DeleteLabelValues(address, clientCN, "success")
	MetricSigningRequestsTotal.DeleteLabelValues(address, clientCN, "error")
}

// IncSigningRequestsTotal increments the request counter for the given labelset.
func IncSigningRequestsTotal(address, clientCN, status string) {
	if isDeletedSigningRequestMetric(address, clientCN) {
		return
	}

	MetricSigningRequestsTotal.WithLabelValues(address, clientCN, status).Inc()
}

func beginSigningRequest(address, clientCN string) {
	labelSet := signingRequestMetricLabelSet{address: address, clientCN: clientCN}

	signingRequestMetricsTracker.mu.Lock()
	signingRequestMetricsTracker.inFlight[labelSet]++
	signingRequestMetricsTracker.mu.Unlock()

	MetricSigningRequestsInFlight.WithLabelValues(address).Inc()
}

func endSigningRequest(address, clientCN string) {
	labelSet := signingRequestMetricLabelSet{address: address, clientCN: clientCN}

	signingRequestMetricsTracker.mu.Lock()
	count := signingRequestMetricsTracker.inFlight[labelSet]
	switch {
	case count <= 1:
		delete(signingRequestMetricsTracker.inFlight, labelSet)
		signingRequestMetricsTracker.cond.Broadcast()
	default:
		signingRequestMetricsTracker.inFlight[labelSet] = count - 1
	}
	signingRequestMetricsTracker.mu.Unlock()

	MetricSigningRequestsInFlight.WithLabelValues(address).Dec()
}

func waitForSigningRequestLabelSetIdle(address, clientCN string) {
	labelSet := signingRequestMetricLabelSet{address: address, clientCN: clientCN}

	signingRequestMetricsTracker.mu.Lock()
	defer signingRequestMetricsTracker.mu.Unlock()

	for signingRequestMetricsTracker.inFlight[labelSet] > 0 {
		signingRequestMetricsTracker.cond.Wait()
	}
}

func isDeletedSigningRequestMetric(address, clientCN string) bool {
	labelSet := signingRequestMetricLabelSet{address: address, clientCN: clientCN}

	value, ok := deletedSigningRequestMetrics.Load(labelSet)
	if !ok {
		return false
	}

	deletedMetric, ok := value.(deletedSigningRequestMetric)
	if !ok {
		deletedSigningRequestMetrics.Delete(labelSet)
		return false
	}

	if time.Now().After(deletedMetric.expiresAt) {
		deletedSigningRequestMetrics.Delete(labelSet)
		return false
	}

	return true
}

func pruneExpiredDeletedSigningRequestMetrics() {
	now := time.Now()
	deletedSigningRequestMetrics.Range(func(key, value any) bool {
		deletedMetric, ok := value.(deletedSigningRequestMetric)
		if !ok || now.After(deletedMetric.expiresAt) {
			deletedSigningRequestMetrics.Delete(key)
		}
		return true
	})
}

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
