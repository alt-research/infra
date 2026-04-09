package service

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestInitKeyMetrics(t *testing.T) {
	address := "0x1234567890abcdef"
	clientCN := "test-client"

	InitKeyMetrics(address, clientCN)

	require.True(t, hasMetricWithLabels(address, clientCN, "success"))
	require.True(t, hasMetricWithLabels(address, clientCN, "error"))

	cleanupSigningRequestMetric(address, clientCN, "success")
	cleanupSigningRequestMetric(address, clientCN, "error")
}

func TestInitKeyMetricsSkipsEmptyClientCN(t *testing.T) {
	address := "0xaaaa"

	InitKeyMetrics(address, "")

	require.False(t, hasMetricWithLabels(address, "", "success"))
	require.False(t, hasMetricWithLabels(address, "", "error"))
}

func TestInitAllKeyMetricsSkipsKeysWithoutClientCN(t *testing.T) {
	keys := []KeyMetricInfo{
		{Address: "0x1111", ClientCN: "client1"},
		{Address: "0x2222", ClientCN: ""},
		{Address: "0x3333", ClientCN: "client3"},
	}

	InitAllKeyMetrics(keys)

	require.True(t, hasMetricWithLabels("0x1111", "client1", "success"))
	require.False(t, hasMetricWithLabels("0x2222", "", "success"))
	require.True(t, hasMetricWithLabels("0x3333", "client3", "success"))

	cleanupSigningRequestMetric("0x1111", "client1", "success")
	cleanupSigningRequestMetric("0x1111", "client1", "error")
	cleanupSigningRequestMetric("0x3333", "client3", "success")
	cleanupSigningRequestMetric("0x3333", "client3", "error")
}

func TestMetricsIncrementAfterInit(t *testing.T) {
	address := "0xdddd"
	clientCN := "increment-test"

	InitKeyMetrics(address, clientCN)

	beforeSuccess := getCounterValue(address, clientCN, "success")
	require.Equal(t, float64(0), beforeSuccess)

	IncSigningRequestsTotal(address, clientCN, "success")

	afterSuccess := getCounterValue(address, clientCN, "success")
	require.Equal(t, float64(1), afterSuccess)

	cleanupSigningRequestMetric(address, clientCN, "success")
	cleanupSigningRequestMetric(address, clientCN, "error")
}

func TestDeleteKeyMetrics(t *testing.T) {
	address := "0xeeee"
	clientCN := "delete-test"

	// Initialize and increment
	InitKeyMetrics(address, clientCN)
	IncSigningRequestsTotal(address, clientCN, "success")
	IncSigningRequestsTotal(address, clientCN, "error")

	// Verify metrics exist with values
	require.Equal(t, float64(1), getCounterValue(address, clientCN, "success"))
	require.Equal(t, float64(1), getCounterValue(address, clientCN, "error"))

	// Delete metrics
	DeleteKeyMetrics(address, clientCN)

	// Verify metrics are deleted
	require.False(t, hasMetricWithLabels(address, clientCN, "success"))
	require.False(t, hasMetricWithLabels(address, clientCN, "error"))
}

func TestDeleteKeyMetricsSkipsEmpty(t *testing.T) {
	// Should not panic with empty values
	DeleteKeyMetrics("", "client")
	DeleteKeyMetrics("0x1234", "")
	DeleteKeyMetrics("", "")
}

func TestDeleteAndReinitResetsCounter(t *testing.T) {
	address := "0xffff"
	clientCN := "reinit-test"

	// Initialize, increment, then delete
	InitKeyMetrics(address, clientCN)
	IncSigningRequestsTotal(address, clientCN, "success")
	require.Equal(t, float64(1), getCounterValue(address, clientCN, "success"))

	DeleteKeyMetrics(address, clientCN)
	require.False(t, hasMetricWithLabels(address, clientCN, "success"))

	// Re-initialize should start from 0
	InitKeyMetrics(address, clientCN)
	require.Equal(t, float64(0), getCounterValue(address, clientCN, "success"))

	// Cleanup
	DeleteKeyMetrics(address, clientCN)
}

func TestDeleteKeyMetricsWaitsForInFlightRequests(t *testing.T) {
	address := "0x9999"
	clientCN := "deleted-test"

	InitKeyMetrics(address, clientCN)
	IncSigningRequestsTotal(address, clientCN, "success")
	beginSigningRequest(address, clientCN)

	deleted := make(chan struct{})
	go func() {
		DeleteKeyMetrics(address, clientCN)
		close(deleted)
	}()

	time.Sleep(10 * time.Millisecond)
	select {
	case <-deleted:
		t.Fatal("DeleteKeyMetrics returned before in-flight requests completed")
	default:
	}

	endSigningRequest(address, clientCN)

	select {
	case <-deleted:
	case <-time.After(time.Second):
		t.Fatal("DeleteKeyMetrics did not finish after in-flight requests completed")
	}

	require.False(t, hasMetricWithLabels(address, clientCN, "success"))
	require.False(t, hasMetricWithLabels(address, clientCN, "error"))
}

func TestIncSigningRequestsTotalSkipsDeletedLabelSet(t *testing.T) {
	address := "0xabcd"
	clientCN := "skip-deleted"

	InitKeyMetrics(address, clientCN)
	DeleteKeyMetrics(address, clientCN)

	IncSigningRequestsTotal(address, clientCN, "success")
	IncSigningRequestsTotal(address, clientCN, "error")

	require.False(t, hasMetricWithLabels(address, clientCN, "success"))
	require.False(t, hasMetricWithLabels(address, clientCN, "error"))

	InitKeyMetrics(address, clientCN)
	IncSigningRequestsTotal(address, clientCN, "success")
	require.Equal(t, float64(1), getCounterValue(address, clientCN, "success"))

	DeleteKeyMetrics(address, clientCN)
}

func cleanupSigningRequestMetric(address, clientCN, status string) {
	MetricSigningRequestsTotal.DeleteLabelValues(address, clientCN, status)
}

func hasMetricWithLabels(address, clientCN, status string) bool {
	return getCounterValue(address, clientCN, status) >= 0
}

func getCounterValue(address, clientCN, status string) float64 {
	registry := prometheus.NewRegistry()
	registry.MustRegister(MetricSigningRequestsTotal)

	metricFamilies, err := registry.Gather()
	if err != nil {
		return -1
	}

	for _, mf := range metricFamilies {
		if mf.GetName() != "signing_requests_total" {
			continue
		}

		for _, m := range mf.GetMetric() {
			labels := m.GetLabel()
			hasAddress := false
			hasClientCN := false
			hasStatus := false

			for _, label := range labels {
				switch label.GetName() {
				case "address":
					hasAddress = label.GetValue() == address
				case "client_cn":
					hasClientCN = label.GetValue() == clientCN
				case "status":
					hasStatus = label.GetValue() == status
				}
			}

			if hasAddress && hasClientCN && hasStatus {
				return m.GetCounter().GetValue()
			}
		}
	}

	return -1
}
