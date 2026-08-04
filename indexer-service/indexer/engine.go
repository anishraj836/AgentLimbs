package indexer

import (
	"sync"

	"github.com/crawler-monorepo/common/index"
	"github.com/crawler-monorepo/common/trie"
)

// IndexEngine coordinates the Inverted Index, Trie Autocomplete, and Document Storage.
type IndexEngine struct {
	mu         sync.RWMutex
	Inverted   *index.InvertedIndex
	Trie       *trie.Trie
	DocTitles  map[string]string // docID -> Title
	DocURLs    map[string]string // docID -> URL
	DocBodies  map[string]string // docID -> Clean Body
}

var GlobalEngine = NewIndexEngine()

func NewIndexEngine() *IndexEngine {
	return &IndexEngine{
		Inverted:  index.NewInvertedIndex(),
		Trie:      trie.NewTrie(),
		DocTitles: make(map[string]string),
		DocURLs:   make(map[string]string),
		DocBodies: make(map[string]string),
	}
}

// IndexDocument indexes a tokenized document into the Inverted Index and Trie.
func (e *IndexEngine) IndexDocument(url, title, cleanBody string, termPositions map[string][]int, totalTokens int) {
	docID := url

	e.mu.Lock()
	e.DocTitles[docID] = title
	e.DocURLs[docID] = url
	e.DocBodies[docID] = cleanBody
	e.mu.Unlock()

	// Update Inverted Index
	e.Inverted.AddDocument(docID, termPositions, totalTokens)

	// Update Trie for Autocomplete
	for term, positions := range termPositions {
		e.Trie.Insert(term, len(positions))
	}
}

// GetDocumentMetadata retrieves stored title, URL, and body for a document ID.
func (e *IndexEngine) GetDocumentMetadata(docID string) (title, url, body string, exists bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	title, exists = e.DocTitles[docID]
	url = e.DocURLs[docID]
	body = e.DocBodies[docID]
	return
}
