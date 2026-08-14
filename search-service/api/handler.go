package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/go-chi/chi/v5"
	"os"
	"crypto/subtle"
)

func checkAuth(w http.ResponseWriter, r *http.Request) bool {
	expectedKey := os.Getenv("AGENT_API_KEY")
	if expectedKey == "" {
		return true
	}
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			apiKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}
	if apiKey == "" || subtle.ConstantTimeCompare([]byte(apiKey), []byte(expectedKey)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"Unauthorized"}`))
		return false
	}
	return true
}

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
	Mode   string `json:"mode,omitempty"`
}

type SearchResponse struct {
	Query     string                  `json:"query"`
	TotalHits int                     `json:"total_hits"`
	LatencyMs float64                 `json:"latency_ms"`
	Results   []search.HybridSearchHit `json:"results"`
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(w, r) { return }
	t0 := time.Now()
	var req SearchRequest

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1*1024*1024)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
			return
		}
	} else {
		req.Query = r.URL.Query().Get("q")
		req.Mode = r.URL.Query().Get("mode")
		req.Limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
		req.Offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	}

	if req.Offset < 0 || req.Offset > 10000 {
		http.Error(w, `{"error":"Invalid offset"}`, http.StatusBadRequest)
		return
	}
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Limit < 1 || req.Limit > 100 {
		http.Error(w, `{"error":"Invalid limit"}`, http.StatusBadRequest)
		return
	}

	fetchK := req.Offset + req.Limit
	titles, urls, bodies := h.engine.GetMetadataMaps()
	bm25Hits := h.engine.Inverted.RankDocuments(
		req.Query,
		titles,
		urls,
		bodies,
		fetchK,
	)

	vecHits := h.engine.SearchVector(req.Query, fetchK)

	fusedHits := search.ReciprocalRankFusion(req.Query, bm25Hits, vecHits, fetchK, titles, urls, bodies)

	if req.Mode == "bm25" {
		filtered := make([]search.HybridSearchHit, 0)
		for _, hit := range fusedHits {
			if hit.BM25Rank > 0 {
				filtered = append(filtered, hit)
			}
		}
		fusedHits = filtered
	} else if req.Mode == "vector" {
		filtered := make([]search.HybridSearchHit, 0)
		for _, hit := range fusedHits {
			if hit.VectorRank > 0 {
				filtered = append(filtered, hit)
			}
		}
		fusedHits = filtered
	}

	totalMatchingHits := len(fusedHits)
	hits := fusedHits
	if req.Offset >= len(hits) {
		hits = []search.HybridSearchHit{}
	} else {
		hits = hits[req.Offset:]
	}
	if len(hits) > req.Limit {
		hits = hits[:req.Limit]
	}

	latency := float64(time.Since(t0).Microseconds()) / 1000.0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SearchResponse{
		Query:     req.Query,
		TotalHits: totalMatchingHits,
		LatencyMs: latency,
		Results:   hits,
	})
}

func (h *SearchHandler) Autocomplete(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(w, r) { return }
	prefix := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 5
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if limit > 50 {
		limit = 50
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
	if !checkAuth(w, r) { return }
	docID := chi.URLParam(r, "*")
	if docID == "" {
		docID = chi.URLParam(r, "id")
	}
	if docID == "" {
		docID = strings.TrimPrefix(r.URL.Path, "/v1/documents/")
	}

	if unescaped, err := url.QueryUnescape(docID); err == nil && unescaped != "" {
		docID = unescaped
	}

	title, urlStr, body, exists := h.engine.GetDocumentMetadata(docID)
	if !exists {
		http.Error(w, `{"error":"Document not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":    docID,
		"title": title,
		"url":   urlStr,
		"body":  body,
	})
}

func (h *SearchHandler) Stats(w http.ResponseWriter, r *http.Request) {
	if !checkAuth(w, r) { return }
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
