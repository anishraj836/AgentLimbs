package index

import (
	"sort"
	"strings"
	"sync"
)

// Trie Autocomplete

type TrieNode struct {
	children  map[rune]*TrieNode
	isEnd     bool
	frequency int
	term      string
}

func newTrieNode() *TrieNode {
	return &TrieNode{
		children: make(map[rune]*TrieNode),
	}
}

type AutocompleteResult struct {
	Term      string `json:"term"`
	Frequency int    `json:"frequency"`
}

type Trie struct {
	mu        sync.RWMutex
	root      *TrieNode
	nodeCount int
}

func NewTrie() *Trie {
	return &Trie{
		root: newTrieNode(),
	}
}

func (t *Trie) Insert(term string, freq int) {
	term = strings.TrimSpace(strings.ToLower(term))
	if term == "" || len([]rune(term)) > 64 {
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

	limit := topK * 10
	if limit < 1000 {
		limit = 1000
	}

	var results []AutocompleteResult
	var dfs func(node *TrieNode)
	dfs = func(node *TrieNode) {
		if node == nil || len(results) >= limit {
			return
		}
		if node.isEnd {
			results = append(results, AutocompleteResult{
				Term:      node.term,
				Frequency: node.frequency,
			})
			if len(results) >= limit {
				return
			}
		}
		for _, child := range node.children {
			dfs(child)
			if len(results) >= limit {
				return
			}
		}
	}
	dfs(curr)

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

func (t *Trie) NodeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.nodeCount
}
