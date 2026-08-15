package cluster

import (
	"errors"
	"sort"
	"strconv"
	"sync"
)

const (
	fnv64Offset uint64 = 14695981039346656037
	fnv64Prime  uint64 = 1099511628211
)

// HashKey computes the 64-bit FNV-1a hash with avalanche mixing for uniform ring distribution.
func HashKey(key string) uint64 {
	var h uint64 = fnv64Offset
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= fnv64Prime
	}
	// 64-bit avalanche mixer
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return h
}

// Token represents a virtual token on the consistent hashing ring.
type Token struct {
	Hash   uint64 `json:"hash"`
	NodeID string `json:"node_id"`
}

// HashRing manages virtual nodes across cluster servers for consistent partition routing.
type HashRing struct {
	mu     sync.RWMutex
	tokens []Token
	vnodes int
	nodes  map[string]bool
}

// ErrEmptyRing is returned when attempting a key lookup on an unpopulated ring.
var ErrEmptyRing = errors.New("cluster: consistent hash ring is empty")

// NewHashRing creates a new consistent hash ring with the specified virtual node count.
func NewHashRing(vnodes int) *HashRing {
	if vnodes <= 0 {
		vnodes = 128
	}
	return &HashRing{
		vnodes: vnodes,
		nodes:  make(map[string]bool),
	}
}

// AddNode adds a physical node to the ring, generating virtual tokens.
func (r *HashRing) AddNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nodes[nodeID] {
		return // Idempotent
	}
	r.nodes[nodeID] = true

	for v := 0; v < r.vnodes; v++ {
		vkey := nodeID + "#" + strconv.Itoa(v)
		r.tokens = append(r.tokens, Token{
			Hash:   HashKey(vkey),
			NodeID: nodeID,
		})
	}

	// Deterministic sort with collision tie-breaker on NodeID
	sort.Slice(r.tokens, func(i, j int) bool {
		if r.tokens[i].Hash == r.tokens[j].Hash {
			return r.tokens[i].NodeID < r.tokens[j].NodeID
		}
		return r.tokens[i].Hash < r.tokens[j].Hash
	})
}

// RemoveNode removes a physical node and all its virtual tokens from the ring.
func (r *HashRing) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.nodes[nodeID] {
		return
	}
	delete(r.nodes, nodeID)

	var filtered []Token
	for _, tok := range r.tokens {
		if tok.NodeID != nodeID {
			filtered = append(filtered, tok)
		}
	}
	r.tokens = filtered
}

// GetNode locates the primary node responsible for the given key.
func (r *HashRing) GetNode(key string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tokens) == 0 {
		return "", ErrEmptyRing
	}

	h := HashKey(key)
	idx := sort.Search(len(r.tokens), func(i int) bool {
		return r.tokens[i].Hash >= h
	})
	if idx == len(r.tokens) {
		idx = 0 // Ring wrap-around
	}
	return r.tokens[idx].NodeID, nil
}

// GetNodes locates up to `count` distinct physical replica nodes responsible for the given key.
func (r *HashRing) GetNodes(key string, count int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tokens) == 0 {
		return nil, ErrEmptyRing
	}
	if count <= 0 {
		return nil, nil
	}

	totalNodes := len(r.nodes)
	if count > totalNodes {
		count = totalNodes
	}

	h := HashKey(key)
	idx := sort.Search(len(r.tokens), func(i int) bool {
		return r.tokens[i].Hash >= h
	})
	if idx == len(r.tokens) {
		idx = 0
	}

	result := make([]string, 0, count)
	seen := make(map[string]bool, count)

	nTokens := len(r.tokens)
	for i := 0; i < nTokens && len(result) < count; i++ {
		pos := (idx + i) % nTokens
		nodeID := r.tokens[pos].NodeID
		if !seen[nodeID] {
			seen[nodeID] = true
			result = append(result, nodeID)
		}
	}

	return result, nil
}

// GetPartition computes the deterministic partition ID (0 to totalPartitions-1) for a key.
func GetPartition(key string, totalPartitions int) int {
	if totalPartitions <= 0 {
		totalPartitions = 16
	}
	return int(HashKey(key) % uint64(totalPartitions))
}

// Nodes returns a list of all registered physical node IDs.
func (r *HashRing) Nodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]string, 0, len(r.nodes))
	for n := range r.nodes {
		list = append(list, n)
	}
	sort.Strings(list)
	return list
}
