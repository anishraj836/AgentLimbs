package trie

import (
	"sort"
	"strings"
	"sync"
)

// TrieNode represents a single node in the prefix Trie.
type TrieNode struct {
	children map[rune]*TrieNode
	isEnd    bool
	frequency int
	term     string
}

func newTrieNode() *TrieNode {
	return &TrieNode{
		children: make(map[rune]*TrieNode),
	}
}

// AutocompleteResult represents a term completion with ranking frequency.
type AutocompleteResult struct {
	Term      string `json:"term"`
	Frequency int    `json:"frequency"`
}

// Trie is a concurrent-safe prefix tree for $O(L)$ autocomplete insertions and lookups.
type Trie struct {
	mu   sync.RWMutex
	root *TrieNode
	nodeCount int
}

func NewTrie() *Trie {
	return &Trie{
		root: newTrieNode(),
	}
}

// Insert adds or updates a term in the Trie with an associated frequency.
func (t *Trie) Insert(term string, freq int) {
	term = strings.TrimSpace(strings.ToLower(term))
	if term == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	curr := t.root
	for _, ch := range term {
		if _, exists := curr.children[ch]; !exists {
			curr.children[ch] = newTrieNode()
			t.nodeCount++
		}
		curr = curr.children[ch]
	}
	curr.isEnd = true
	curr.frequency += freq
	curr.term = term
}

// SearchPrefix finds the Top-K terms starting with prefix in $O(L)$ time.
func (t *Trie) SearchPrefix(prefix string, topK int) []AutocompleteResult {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" || topK <= 0 {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	for _, ch := range prefix {
		next, exists := curr.children[ch]
		if !exists {
			return nil
		}
		curr = next
	}

	// DFS to gather all completions starting from current prefix node
	var results []AutocompleteResult
	var dfs func(node *TrieNode)
	dfs = func(node *TrieNode) {
		if node == nil {
			return
		}
		if node.isEnd {
			results = append(results, AutocompleteResult{
				Term:      node.term,
				Frequency: node.frequency,
			})
		}
		for _, child := range node.children {
			dfs(child)
		}
	}
	dfs(curr)

	// Sort results by frequency descending, then term ascending
	sort.Slice(results, func(i, j int) bool {
		if results[i].Frequency == results[j].Frequency {
			return results[i].Term < results[j].Term
		}
		return results[i].Frequency > results[j].Frequency
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}

// NodeCount returns the total number of nodes in the Trie.
func (t *Trie) NodeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodeCount
}
