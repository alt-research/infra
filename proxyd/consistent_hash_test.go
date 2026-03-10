package proxyd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestBackends(names ...string) []*Backend {
	backends := make([]*Backend, len(names))
	for i, name := range names {
		backends[i] = &Backend{Name: name}
	}
	return backends
}

func TestConsistentHash_SameKeyReturnsSameOrder(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1", "node-2", "node-3")
	ch.Update(backends)

	key := "192.168.1.100"
	first := ch.GetOrderedBackends(key, backends)
	for i := 0; i < 100; i++ {
		result := ch.GetOrderedBackends(key, backends)
		require.Equal(t, len(first), len(result))
		for j := range first {
			assert.Equal(t, first[j].Name, result[j].Name,
				"order should be deterministic for the same key")
		}
	}
}

func TestConsistentHash_DifferentKeysDistribute(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1", "node-2", "node-3")
	ch.Update(backends)

	counts := make(map[string]int)
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		result := ch.GetOrderedBackends(key, backends)
		counts[result[0].Name]++
	}

	// Each backend should get at least 20% of keys (expect ~33%)
	for _, name := range []string{"node-1", "node-2", "node-3"} {
		assert.Greater(t, counts[name], numKeys/5,
			"backend %s should get a reasonable share of traffic", name)
	}
}

func TestConsistentHash_BackendAddMinimalDisruption(t *testing.T) {
	ch := NewConsistentHash(150)
	backends3 := makeTestBackends("node-1", "node-2", "node-3")
	ch.Update(backends3)

	// Record mapping for 1000 keys
	numKeys := 1000
	originalMapping := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		result := ch.GetOrderedBackends(key, backends3)
		originalMapping[key] = result[0].Name
	}

	// Add a 4th backend
	backends4 := makeTestBackends("node-1", "node-2", "node-3", "node-4")
	ch.Update(backends4)

	changed := 0
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		result := ch.GetOrderedBackends(key, backends4)
		if result[0].Name != originalMapping[key] {
			changed++
		}
	}

	// Adding 1 of 4 backends should ideally move ~25% of keys
	// Allow up to 40% disruption
	assert.Less(t, changed, numKeys*40/100,
		"adding one backend should cause minimal disruption")
}

func TestConsistentHash_BackendRemoveMinimalDisruption(t *testing.T) {
	ch := NewConsistentHash(150)
	backends4 := makeTestBackends("node-1", "node-2", "node-3", "node-4")
	ch.Update(backends4)

	numKeys := 1000
	originalMapping := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		result := ch.GetOrderedBackends(key, backends4)
		originalMapping[key] = result[0].Name
	}

	// Remove node-3
	backends3 := makeTestBackends("node-1", "node-2", "node-4")
	ch.Update(backends3)

	changed := 0
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		result := ch.GetOrderedBackends(key, backends3)
		if result[0].Name != originalMapping[key] {
			changed++
		}
	}

	// Removing 1 of 4 should ideally only affect ~25% of keys
	assert.Less(t, changed, numKeys*40/100,
		"removing one backend should cause minimal disruption")
}

func TestConsistentHash_EmptyCandidates(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1", "node-2")
	ch.Update(backends)

	result := ch.GetOrderedBackends("some-key", nil)
	assert.Empty(t, result)

	result = ch.GetOrderedBackends("some-key", []*Backend{})
	assert.Empty(t, result)
}

func TestConsistentHash_EmptyRing(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1")
	// Ring not updated, should return candidates as-is
	result := ch.GetOrderedBackends("some-key", backends)
	assert.Equal(t, backends, result)
}

func TestConsistentHash_SingleBackend(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1")
	ch.Update(backends)

	result := ch.GetOrderedBackends("any-key", backends)
	require.Len(t, result, 1)
	assert.Equal(t, "node-1", result[0].Name)
}

func TestConsistentHash_SubsetOfCandidates(t *testing.T) {
	ch := NewConsistentHash(150)
	allBackends := makeTestBackends("node-1", "node-2", "node-3", "node-4")
	ch.Update(allBackends)

	// Only pass a subset as candidates (simulating some backends are unhealthy)
	healthyCandidates := makeTestBackends("node-1", "node-3")
	result := ch.GetOrderedBackends("some-key", healthyCandidates)

	require.Len(t, result, 2)
	// Both candidates should be in result
	names := make(map[string]bool)
	for _, be := range result {
		names[be.Name] = true
	}
	assert.True(t, names["node-1"])
	assert.True(t, names["node-3"])
}

func TestConsistentHash_UpdateNoChange(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1", "node-2")
	ch.Update(backends)

	ringLen := len(ch.ring)

	// Update with same backends (different slice, same names)
	backends2 := makeTestBackends("node-1", "node-2")
	ch.Update(backends2)

	// Ring should not have been rebuilt (same length)
	assert.Equal(t, ringLen, len(ch.ring))
}

func TestConsistentHash_ReturnsAllCandidates(t *testing.T) {
	ch := NewConsistentHash(150)
	backends := makeTestBackends("node-1", "node-2", "node-3")
	ch.Update(backends)

	result := ch.GetOrderedBackends("test-key", backends)
	require.Len(t, result, 3, "should return all candidates in ordered list")

	seen := make(map[string]bool)
	for _, be := range result {
		seen[be.Name] = true
	}
	assert.Len(t, seen, 3, "all backends should appear exactly once")
}

func TestConsistentHash_DefaultVirtualNodes(t *testing.T) {
	ch := NewConsistentHash(0)
	assert.Equal(t, defaultVirtualNodes, ch.virtualNodes)

	ch2 := NewConsistentHash(-1)
	assert.Equal(t, defaultVirtualNodes, ch2.virtualNodes)
}
