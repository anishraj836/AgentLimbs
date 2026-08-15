package index

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestVectorIndex_AddVectorBatch(t *testing.T) {
	vi := NewVectorIndex(4)

	docIDs := []string{"doc1", "doc2"}
	vectors := [][]float64{
		{0.1, 0.2, 0.3, 0.4},
		{0.5, 0.6, 0.7, 0.8},
	}

	err := vi.AddVectorBatch(docIDs, vectors)
	if err != nil {
		t.Fatalf("AddVectorBatch failed: %v", err)
	}

	results := vi.SearchNearest([]float64{0.1, 0.2, 0.3, 0.4}, 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 search results, got %d", len(results))
	}
	if results[0].DocID != "doc1" {
		t.Errorf("expected doc1 as top result, got %s", results[0].DocID)
	}

	// Dimension mismatch atomic rejection test
	badVectors := [][]float64{
		{0.1, 0.2, 0.3, 0.4},
		{0.5, 0.6}, // 2 dims instead of 4
	}
	err = vi.AddVectorBatch([]string{"doc3", "doc4"}, badVectors)
	if err == nil {
		t.Fatal("expected dimension mismatch error for AddVectorBatch, got nil")
	}

	// Verify doc3 was NOT inserted due to atomic pre-validation
	resultsAfter := vi.SearchNearest([]float64{0.1, 0.2, 0.3, 0.4}, 10)
	if len(resultsAfter) != 2 {
		t.Errorf("expected still 2 vectors in index after failed atomic batch, got %d", len(resultsAfter))
	}

	// Length mismatch error test
	err = vi.AddVectorBatch([]string{"doc5"}, vectors)
	if err == nil {
		t.Fatal("expected length mismatch error, got nil")
	}

	// Empty batch should be no-op
	if err := vi.AddVectorBatch(nil, nil); err != nil {
		t.Errorf("expected nil error on empty batch, got %v", err)
	}

	// Provider name and close
	if vi.ProviderName() != "memory_flat" {
		t.Errorf("expected provider memory_flat, got %s", vi.ProviderName())
	}
	if err := vi.Close(); err != nil {
		t.Errorf("expected nil close error, got %v", err)
	}
}

func TestVectorIndex_NilReceiverSafety(t *testing.T) {
	var vi *VectorIndex = nil

	if vi.Dimensions() != 0 {
		t.Errorf("expected 0 dimensions on nil receiver, got %d", vi.Dimensions())
	}
	if vi.ProviderName() != "memory_flat" {
		t.Errorf("expected memory_flat on nil receiver, got %s", vi.ProviderName())
	}
	if err := vi.Close(); err != nil {
		t.Errorf("expected nil error on Close, got %v", err)
	}
	if err := vi.AddVector("d1", []float64{1.0}); err == nil {
		t.Error("expected error on AddVector with nil receiver")
	}
	if err := vi.AddVectorBatch([]string{"d1"}, [][]float64{{1.0}}); err == nil {
		t.Error("expected error on AddVectorBatch with nil receiver")
	}
	vi.DeleteVector("d1") // Should not panic
	if res := vi.SearchNearest([]float64{1.0}, 5); res != nil {
		t.Errorf("expected nil result on SearchNearest with nil receiver, got %v", res)
	}
	if err := vi.SaveSnapshot("temp.json"); err == nil {
		t.Error("expected error on SaveSnapshot with nil receiver")
	}
	if err := vi.LoadSnapshot("temp.json"); err == nil {
		t.Error("expected error on LoadSnapshot with nil receiver")
	}
}

func TestQdrantVectorStore_CRUDAndSearch(t *testing.T) {
	var receivedPoints []qdrantPoint
	var searchCalled bool
	var deleteCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "PUT" && r.URL.Path == "/collections/testcol/points":
			var req qdrantUpsertRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			receivedPoints = req.Points
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":{"status":"ok"},"status":"ok"}`))

		case r.Method == "POST" && r.URL.Path == "/collections/testcol/points/search":
			searchCalled = true
			resp := qdrantSearchResponse{
				Result: []struct {
					ID      interface{}            `json:"id"`
					Score   float64                `json:"score"`
					Payload map[string]interface{} `json:"payload"`
				}{
					{
						ID:    "point-1",
						Score: 0.9543,
						Payload: map[string]interface{}{
							"doc_id": "https://example.com/doc1",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "POST" && r.URL.Path == "/collections/testcol/points/delete":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":{"status":"ok"},"status":"ok"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	store := NewQdrantVectorStore(4).
		WithBaseURL(server.URL).
		WithCollectionName("testcol").
		WithHTTPClient(server.Client())

	if store.Dimensions() != 4 {
		t.Errorf("expected 4 dimensions, got %d", store.Dimensions())
	}
	if store.ProviderName() != "qdrant" {
		t.Errorf("expected qdrant provider name, got %s", store.ProviderName())
	}

	// Add single vector
	err := store.AddVector("https://example.com/doc1", []float64{0.1, 0.2, 0.3, 0.4})
	if err != nil {
		t.Fatalf("AddVector failed: %v", err)
	}
	if len(receivedPoints) != 1 {
		t.Fatalf("expected 1 point received by mock server, got %d", len(receivedPoints))
	}
	if receivedPoints[0].Payload["doc_id"] != "https://example.com/doc1" {
		t.Errorf("expected payload doc_id 'https://example.com/doc1', got %v", receivedPoints[0].Payload["doc_id"])
	}

	// Search
	results := store.SearchNearest([]float64{0.1, 0.2, 0.3, 0.4}, 5)
	if !searchCalled {
		t.Fatal("expected search endpoint to be called")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(results))
	}
	if results[0].DocID != "https://example.com/doc1" {
		t.Errorf("expected docID https://example.com/doc1, got %s", results[0].DocID)
	}
	if results[0].Similarity != 0.9543 {
		t.Errorf("expected similarity 0.9543, got %f", results[0].Similarity)
	}

	// Delete
	store.DeleteVector("https://example.com/doc1")
	if !deleteCalled {
		t.Fatal("expected delete endpoint to be called")
	}

	// Close
	if err := store.Close(); err != nil {
		t.Errorf("expected nil Close error, got %v", err)
	}

	// Dimension mismatch error
	err = store.AddVector("doc2", []float64{0.1, 0.2})
	if err == nil {
		t.Fatal("expected error on dimension mismatch, got nil")
	}
}

func TestQdrantVectorStore_NilSafety(t *testing.T) {
	var q *QdrantVectorStore = nil

	if q.Dimensions() != 0 {
		t.Errorf("expected 0 dims, got %d", q.Dimensions())
	}
	if q.ProviderName() != "qdrant" {
		t.Errorf("expected qdrant provider name, got %s", q.ProviderName())
	}
	if err := q.Close(); err != nil {
		t.Errorf("expected nil error on Close, got %v", err)
	}
	if err := q.AddVector("d1", []float64{1.0}); err == nil {
		t.Error("expected error on AddVector")
	}
	if err := q.AddVectorBatch([]string{"d1"}, [][]float64{{1.0}}); err == nil {
		t.Error("expected error on AddVectorBatch")
	}
	q.DeleteVector("d1") // Should not panic
	if res := q.SearchNearest([]float64{1.0}, 5); res != nil {
		t.Errorf("expected nil results, got %v", res)
	}
}

func TestChromaVectorStore_CRUDAndSearch(t *testing.T) {
	var addCalled bool
	var queryCalled bool
	var deleteCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/collections/testcol/add":
			addCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))

		case r.Method == "POST" && r.URL.Path == "/api/v1/collections/testcol/query":
			queryCalled = true
			resp := chromaQueryResponse{
				IDs:       [][]string{{"https://example.com/page1", "https://example.com/page2"}},
				Distances: [][]float64{{0.05, 0.2}},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case r.Method == "POST" && r.URL.Path == "/api/v1/collections/testcol/delete":
			deleteCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"success"}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	store := NewChromaVectorStore(3).
		WithBaseURL(server.URL).
		WithCollection("testcol").
		WithHTTPClient(server.Client())

	if store.Dimensions() != 3 {
		t.Errorf("expected 3 dimensions, got %d", store.Dimensions())
	}
	if store.ProviderName() != "chroma" {
		t.Errorf("expected chroma provider name, got %s", store.ProviderName())
	}

	// Add batch
	err := store.AddVectorBatch([]string{"d1", "d2"}, [][]float64{
		{0.1, 0.2, 0.3},
		{0.4, 0.5, 0.6},
	})
	if err != nil {
		t.Fatalf("AddVectorBatch failed: %v", err)
	}
	if !addCalled {
		t.Fatal("expected add endpoint to be called")
	}

	// Search
	results := store.SearchNearest([]float64{0.1, 0.2, 0.3}, 2)
	if !queryCalled {
		t.Fatal("expected query endpoint to be called")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].DocID != "https://example.com/page1" {
		t.Errorf("expected doc1, got %s", results[0].DocID)
	}
	if results[0].Similarity != 0.95 {
		t.Errorf("expected similarity 0.95 (1 - 0.05), got %f", results[0].Similarity)
	}

	// Delete
	store.DeleteVector("d1")
	if !deleteCalled {
		t.Fatal("expected delete endpoint to be called")
	}

	if err := store.Close(); err != nil {
		t.Errorf("expected nil Close error, got %v", err)
	}
}

func TestChromaVectorStore_NilSafety(t *testing.T) {
	var c *ChromaVectorStore = nil

	if c.Dimensions() != 0 {
		t.Errorf("expected 0 dims, got %d", c.Dimensions())
	}
	if c.ProviderName() != "chroma" {
		t.Errorf("expected chroma provider name, got %s", c.ProviderName())
	}
	if err := c.Close(); err != nil {
		t.Errorf("expected nil error on Close, got %v", err)
	}
	if err := c.AddVector("d1", []float64{1.0}); err == nil {
		t.Error("expected error on AddVector")
	}
	if err := c.AddVectorBatch([]string{"d1"}, [][]float64{{1.0}}); err == nil {
		t.Error("expected error on AddVectorBatch")
	}
	c.DeleteVector("d1")
	if res := c.SearchNearest([]float64{1.0}, 5); res != nil {
		t.Errorf("expected nil results, got %v", res)
	}
}

func TestPgVectorStore_BasicsAndNilSafety(t *testing.T) {
	store := NewPgVectorStore(128)
	if store.Dimensions() != 128 {
		t.Errorf("expected 128 dims, got %d", store.Dimensions())
	}
	if store.ProviderName() != "pgvector" {
		t.Errorf("expected pgvector provider name, got %s", store.ProviderName())
	}

	// Uninitialized db error check
	err := store.AddVector("d1", make([]float64, 128))
	if err == nil {
		t.Error("expected error when DB is uninitialized")
	}
	store.DeleteVector("d1") // Should not panic
	if res := store.SearchNearest(make([]float64, 128), 5); res != nil {
		t.Errorf("expected nil search results with uninitialized DB, got %v", res)
	}
	if err := store.Close(); err != nil {
		t.Errorf("expected nil close error, got %v", err)
	}

	// Vector formatting helper
	formatted := formatVectorForPg([]float64{1.5, 2.5, 3.5})
	if formatted != "[1.500000,2.500000,3.500000]" {
		t.Errorf("unexpected formatted vector: %s", formatted)
	}

	// Nil receiver safety
	var p *PgVectorStore = nil
	if p.Dimensions() != 0 {
		t.Errorf("expected 0 dims, got %d", p.Dimensions())
	}
	if p.ProviderName() != "pgvector" {
		t.Errorf("expected pgvector, got %s", p.ProviderName())
	}
	if err := p.Close(); err != nil {
		t.Errorf("expected nil close error, got %v", err)
	}
	if err := p.AddVector("d1", []float64{1.0}); err == nil {
		t.Error("expected error on AddVector")
	}
}

func TestNewVectorStoreFromEnv(t *testing.T) {
	os.Setenv("VECTOR_STORE_PROVIDER", "qdrant")
	s1 := NewVectorStoreFromEnv(256)
	if s1.ProviderName() != "qdrant" || s1.Dimensions() != 256 {
		t.Errorf("expected Qdrant vector store with 256 dims, got %s (%d)", s1.ProviderName(), s1.Dimensions())
	}

	os.Setenv("VECTOR_STORE_PROVIDER", "chroma")
	s2 := NewVectorStoreFromEnv(512)
	if s2.ProviderName() != "chroma" || s2.Dimensions() != 512 {
		t.Errorf("expected Chroma vector store with 512 dims, got %s (%d)", s2.ProviderName(), s2.Dimensions())
	}

	os.Setenv("VECTOR_STORE_PROVIDER", "pgvector")
	s3 := NewVectorStoreFromEnv(768)
	if s3.ProviderName() != "pgvector" || s3.Dimensions() != 768 {
		t.Errorf("expected PgVector vector store with 768 dims, got %s (%d)", s3.ProviderName(), s3.Dimensions())
	}

	os.Unsetenv("VECTOR_STORE_PROVIDER")
	os.Unsetenv("VECTOR_STORE")
	s4 := NewVectorStoreFromEnv(128)
	if s4.ProviderName() != "memory_flat" || s4.Dimensions() != 128 {
		t.Errorf("expected default memory_flat vector store with 128 dims, got %s (%d)", s4.ProviderName(), s4.Dimensions())
	}
}
