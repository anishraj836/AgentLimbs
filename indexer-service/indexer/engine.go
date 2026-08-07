package indexer

import (
	"context"
	"sync"

	"github.com/crawler-monorepo/common/db"
	"github.com/crawler-monorepo/common/index"
	"github.com/crawler-monorepo/common/trie"
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
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

// IndexDocument indexes a tokenized document into the Inverted Index, Trie, and PostgreSQL using default web_crawled source.
func (e *IndexEngine) IndexDocument(url, title, cleanBody string, termPositions map[string][]int, totalTokens int) {
	e.IndexDocumentWithSource(url, title, cleanBody, termPositions, totalTokens, "web_crawled", url)
}

// IndexDocumentWithSource indexes a tokenized document with explicit data lineage (sourceType and sourceURL).
func (e *IndexEngine) IndexDocumentWithSource(url, title, cleanBody string, termPositions map[string][]int, totalTokens int, sourceType, sourceURL string) {
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

	// Persist to PostgreSQL shared database with Data Lineage
	_ = db.SaveCrawledDocument(context.Background(), url, title, cleanBody, totalTokens, sourceType, sourceURL)
}

// LoadFromDB loads all persisted documents from PostgreSQL into the in-memory index on microservice startup.
func (e *IndexEngine) LoadFromDB(ctx context.Context) error {
	docs, err := db.GetCrawledDocuments(ctx)
	if err != nil || len(docs) == 0 {
		return err
	}

	for _, d := range docs {
		// Tokenize clean body and index
		tokDoc := tokenizer.TokenizePipeline(d.URL, d.Title, d.CleanBody)
		e.mu.Lock()
		e.DocTitles[d.URL] = d.Title
		e.DocURLs[d.URL] = d.URL
		e.DocBodies[d.URL] = d.CleanBody
		e.mu.Unlock()

		e.Inverted.AddDocument(d.URL, tokDoc.TermPositions, tokDoc.TotalTokens)
		for term, positions := range tokDoc.TermPositions {
			e.Trie.Insert(term, len(positions))
		}
	}
	return nil
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

// GetMetadataMaps returns thread-safe copies of DocTitles, DocURLs, and DocBodies to prevent data races.
func (e *IndexEngine) GetMetadataMaps() (titles, urls, bodies map[string]string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	titles = make(map[string]string, len(e.DocTitles))
	urls = make(map[string]string, len(e.DocURLs))
	bodies = make(map[string]string, len(e.DocBodies))

	for k, v := range e.DocTitles {
		titles[k] = v
	}
	for k, v := range e.DocURLs {
		urls[k] = v
	}
	for k, v := range e.DocBodies {
		bodies[k] = v
	}

	return titles, urls, bodies
}
