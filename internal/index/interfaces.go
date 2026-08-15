package index

import "context"

// Embedder generates dense vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
	Dimensions() int
	ProviderName() string
}

// Searcher defines search capabilities for keyword/BM25 queries.
type Searcher interface {
	SearchBM25(query string, topK int) []SearchHit
}

// VectorStore defines operations for adding, deleting, and querying vector embeddings.
type VectorStore interface {
	AddVector(docID string, vec []float64) error
	AddVectorBatch(docIDs []string, vectors [][]float64) error
	DeleteVector(docID string)
	SearchNearest(queryVector []float64, topK int) []VectorSearchResult
	Dimensions() int
	ProviderName() string
	Close() error
}

// Autocompleter defines prefix search and prefix insertion for auto-completion.
type Autocompleter interface {
	Insert(term string, freq int)
	SearchPrefix(prefix string, topK int) []AutocompleteResult
	NodeCount() int
}

// MetadataReader provides access to indexed document metadata and URL alias resolution.
type MetadataReader interface {
	GetDocumentMetadata(docID string) (title, url, body string, exists bool)
	GetMetadataMaps() (titles, urls, bodies map[string]string)
	ResolveURL(targetURL string) string
}
