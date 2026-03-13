package proxyd

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

const defaultVirtualNodes = 150

// ConsistentHash implements consistent hashing with virtual nodes
// for sticky session routing of backends.
type ConsistentHash struct {
	mu           sync.RWMutex
	virtualNodes int
	ring         []uint32
	hashMap      map[uint32]*Backend
	backends     map[string]int // track current backend names and weights for change detection
}

func NewConsistentHash(virtualNodes int) *ConsistentHash {
	if virtualNodes <= 0 {
		virtualNodes = defaultVirtualNodes
	}
	return &ConsistentHash{
		virtualNodes: virtualNodes,
		ring:         make([]uint32, 0),
		hashMap:      make(map[uint32]*Backend),
		backends:     make(map[string]int),
	}
}

func (ch *ConsistentHash) hashKey(key string) uint32 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint32(h[:4])
}

// Update rebuilds the hash ring with the given backends.
// It skips rebuilding if the backend set (names and weights) hasn't changed.
// Each backend gets virtualNodes * max(weight, 1) virtual nodes on the ring,
// so higher-weight backends attract proportionally more traffic.
func (ch *ConsistentHash) Update(backends []*Backend) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.hasChanged(backends) {
		return
	}

	totalVNodes := 0
	for _, be := range backends {
		totalVNodes += ch.virtualNodes * effectiveWeight(be)
	}

	ch.ring = make([]uint32, 0, totalVNodes)
	ch.hashMap = make(map[uint32]*Backend, totalVNodes)
	ch.backends = make(map[string]int, len(backends))

	for _, be := range backends {
		w := effectiveWeight(be)
		ch.backends[be.Name] = be.weight
		for i := 0; i < ch.virtualNodes*w; i++ {
			vkey := fmt.Sprintf("%s#%d", be.Name, i)
			hash := ch.hashKey(vkey)
			ch.ring = append(ch.ring, hash)
			ch.hashMap[hash] = be
		}
	}

	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})
}

// effectiveWeight returns the backend's weight, defaulting to 1 if unset or zero.
func effectiveWeight(be *Backend) int {
	if be.weight <= 0 {
		return 1
	}
	return be.weight
}

func (ch *ConsistentHash) hasChanged(backends []*Backend) bool {
	if len(backends) != len(ch.backends) {
		return true
	}
	for _, be := range backends {
		prevWeight, exists := ch.backends[be.Name]
		if !exists {
			return true
		}
		if be.weight != prevWeight {
			return true
		}
	}
	return false
}

// GetOrderedBackends returns candidates ordered by consistent hash proximity to clientKey.
// The first element is the preferred backend; subsequent elements are fallbacks.
func (ch *ConsistentHash) GetOrderedBackends(clientKey string, candidates []*Backend) []*Backend {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 || len(candidates) == 0 {
		return candidates
	}

	clientHash := ch.hashKey(clientKey)

	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= clientHash
	})
	if idx >= len(ch.ring) {
		idx = 0
	}

	// Build a set of candidate names for quick lookup
	candidateSet := make(map[string]*Backend, len(candidates))
	for _, be := range candidates {
		candidateSet[be.Name] = be
	}

	result := make([]*Backend, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))

	// Walk the ring clockwise collecting unique candidates
	for i := 0; i < len(ch.ring) && len(result) < len(candidates); i++ {
		ringIdx := (idx + i) % len(ch.ring)
		be := ch.hashMap[ch.ring[ringIdx]]
		if !seen[be.Name] {
			if _, ok := candidateSet[be.Name]; ok {
				result = append(result, be)
				seen[be.Name] = true
			}
		}
	}

	// Defensive: append any candidates not found on the ring
	for _, be := range candidates {
		if !seen[be.Name] {
			result = append(result, be)
		}
	}

	return result
}
