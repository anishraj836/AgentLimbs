package trie

import (
	"github.com/crawler-monorepo/internal/index"
)

type TrieNode = index.TrieNode
type AutocompleteResult = index.AutocompleteResult
type Trie = index.Trie

func NewTrie() *Trie {
	return index.NewTrie()
}
