package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/go-chi/chi/v5"
)

type SearchHandler struct {
	engine *index.Engine
}

func NewSearchHandler(engine *index.Engine) *SearchHandler {
	return &SearchHandler{engine: engine}
}

type SearchRequest struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type SearchResponse struct {
	Query     string                  `json:"query"`
	TotalHits int                     `json:"total_hits"`
	LatencyMs float64                 `json:"latency_ms"`
	Results   []search.HybridSearchHit `json:"results"`
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()
	var req SearchRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
	} else {
		req.Query = r.URL.Query().Get("q")
		req.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	bm25Hits := h.engine.Inverted.RankDocuments(
		req.Query,
		h.engine.DocTitles,
		h.engine.DocURLs,
		h.engine.DocBodies,
		req.Limit,
	)
	vectorHits := h.engine.SearchVector(req.Query, req.Limit)
	hits := search.ReciprocalRankFusion(bm25Hits, vectorHits, req.Limit)

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SearchResponse{
		Query:     req.Query,
		TotalHits: len(hits),
		LatencyMs: latency,
		Results:   hits,
	})
}

func (h *SearchHandler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 5
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	results := h.engine.Trie.SearchPrefix(prefix, limit)
	if results == nil {
		results = make([]index.AutocompleteResult, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"prefix":      prefix,
		"suggestions": results,
	})
}

func (h *SearchHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "id")
	title, url, body, exists := h.engine.GetDocumentMetadata(docID)
	if !exists {
		http.Error(w, `{"error":"Document not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":    docID,
		"title": title,
		"url":   url,
		"body":  body,
	})
}

func (h *SearchHandler) Stats(w http.ResponseWriter, r *http.Request) {
	totalDocs, avgDocLen, vocabSize := h.engine.Inverted.GetStats()
	trieNodeCount := h.engine.Trie.NodeCount()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_documents": totalDocs,
		"avg_doc_length":  avgDocLen,
		"vocabulary_size": vocabSize,
		"trie_node_count": trieNodeCount,
	})
}

func (h *SearchHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"search"}`))
}
