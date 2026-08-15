package index

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/logger"
	"go.uber.org/zap"
)

// Compile-time interface compliance assertions
var (
	_ VectorStore = (*VectorIndex)(nil)
	_ VectorStore = (*QdrantVectorStore)(nil)
	_ VectorStore = (*ChromaVectorStore)(nil)
	_ VectorStore = (*PgVectorStore)(nil)
)

// docIDToUUID deterministically converts any string docID into a RFC 4122 compliant UUID format.
func docIDToUUID(docID string) string {
	h := md5.Sum([]byte(docID))
	hexStr := hex.EncodeToString(h[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

// -----------------------------------------------------------------------------
// Qdrant Vector Store Adapter
// -----------------------------------------------------------------------------

// QdrantVectorStore implements VectorStore backed by Qdrant HNSW vector search.
type QdrantVectorStore struct {
	baseURL        string
	collectionName string
	apiKey         string
	dimensions     int
	client         *http.Client
}

// NewQdrantVectorStore creates a new QdrantVectorStore configured from environment variables or defaults.
func NewQdrantVectorStore(dim int) *QdrantVectorStore {
	if dim <= 0 {
		dim = 128
	}
	baseURL := os.Getenv("QDRANT_URL")
	if baseURL == "" {
		baseURL = os.Getenv("QDRANT_HOST")
	}
	if baseURL == "" {
		baseURL = "http://localhost:6333"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	collection := os.Getenv("QDRANT_COLLECTION")
	if collection == "" {
		collection = "weblimb"
	}

	apiKey := os.Getenv("QDRANT_API_KEY")

	return &QdrantVectorStore{
		baseURL:        baseURL,
		collectionName: collection,
		apiKey:         apiKey,
		dimensions:     dim,
		client:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (q *QdrantVectorStore) WithHTTPClient(client *http.Client) *QdrantVectorStore {
	if q != nil {
		q.client = client
	}
	return q
}

func (q *QdrantVectorStore) WithBaseURL(baseURL string) *QdrantVectorStore {
	if q != nil {
		q.baseURL = strings.TrimRight(baseURL, "/")
	}
	return q
}

func (q *QdrantVectorStore) WithCollectionName(name string) *QdrantVectorStore {
	if q != nil {
		q.collectionName = name
	}
	return q
}

func (q *QdrantVectorStore) Dimensions() int {
	if q == nil {
		return 0
	}
	return q.dimensions
}

func (q *QdrantVectorStore) ProviderName() string {
	return "qdrant"
}

func (q *QdrantVectorStore) Close() error {
	return nil
}

type qdrantPoint struct {
	ID      string                 `json:"id"`
	Vector  []float64              `json:"vector"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type qdrantUpsertRequest struct {
	Points []qdrantPoint `json:"points"`
}

func (q *QdrantVectorStore) AddVector(docID string, vec []float64) error {
	if q == nil {
		return fmt.Errorf("qdrant vector store is nil")
	}
	return q.AddVectorBatch([]string{docID}, [][]float64{vec})
}

func (q *QdrantVectorStore) AddVectorBatch(docIDs []string, vectors [][]float64) error {
	if q == nil {
		return fmt.Errorf("qdrant vector store is nil")
	}
	if len(docIDs) != len(vectors) {
		return fmt.Errorf("length mismatch: %d docIDs vs %d vectors", len(docIDs), len(vectors))
	}
	if len(docIDs) == 0 {
		return nil
	}

	for i, vec := range vectors {
		if len(vec) != q.dimensions {
			return fmt.Errorf("vector dimension mismatch at index %d: expected %d, got %d", i, q.dimensions, len(vec))
		}
	}

	points := make([]qdrantPoint, len(docIDs))
	for i, docID := range docIDs {
		points[i] = qdrantPoint{
			ID:     docIDToUUID(docID),
			Vector: vectors[i],
			Payload: map[string]interface{}{
				"doc_id": docID,
			},
		}
	}

	reqBody, err := json.Marshal(qdrantUpsertRequest{Points: points})
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("%s/collections/%s/points?wait=true", q.baseURL, q.collectionName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}

	client := q.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("qdrant upsert returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (q *QdrantVectorStore) DeleteVector(docID string) {
	if q == nil || docID == "" {
		return
	}

	payload := map[string]interface{}{
		"points": []string{docIDToUUID(docID)},
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return
	}

	apiURL := fmt.Sprintf("%s/collections/%s/points/delete?wait=true", q.baseURL, q.collectionName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}

	client := q.client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	_ = resp.Body.Close()
}

type qdrantSearchResponse struct {
	Result []struct {
		ID      interface{}            `json:"id"`
		Score   float64                `json:"score"`
		Payload map[string]interface{} `json:"payload"`
	} `json:"result"`
}

func (q *QdrantVectorStore) SearchNearest(queryVector []float64, topK int) []VectorSearchResult {
	if q == nil || len(queryVector) != q.dimensions || topK <= 0 {
		return nil
	}

	payload := map[string]interface{}{
		"vector":       queryVector,
		"limit":        topK,
		"with_payload": true,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	apiURL := fmt.Sprintf("%s/collections/%s/points/search", q.baseURL, q.collectionName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}

	client := q.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var res qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil
	}

	var hits []VectorSearchResult
	for _, item := range res.Result {
		docID := ""
		if item.Payload != nil {
			if d, ok := item.Payload["doc_id"].(string); ok && d != "" {
				docID = d
			}
		}
		if docID == "" {
			docID = fmt.Sprintf("%v", item.ID)
		}

		hits = append(hits, VectorSearchResult{
			DocID:      docID,
			Similarity: math.Round(item.Score*10000) / 10000,
		})
	}

	return hits
}

// -----------------------------------------------------------------------------
// Chroma Vector Store Adapter
// -----------------------------------------------------------------------------

// ChromaVectorStore implements VectorStore backed by ChromaDB HTTP API.
type ChromaVectorStore struct {
	baseURL        string
	collectionName string
	collectionID   string
	dimensions     int
	client         *http.Client
}

// NewChromaVectorStore creates a new ChromaVectorStore configured from environment variables or defaults.
func NewChromaVectorStore(dim int) *ChromaVectorStore {
	if dim <= 0 {
		dim = 128
	}
	baseURL := os.Getenv("CHROMA_URL")
	if baseURL == "" {
		baseURL = os.Getenv("CHROMA_HOST")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	collection := os.Getenv("CHROMA_COLLECTION")
	if collection == "" {
		collection = "weblimb"
	}

	return &ChromaVectorStore{
		baseURL:        baseURL,
		collectionName: collection,
		collectionID:   collection,
		dimensions:     dim,
		client:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *ChromaVectorStore) WithHTTPClient(client *http.Client) *ChromaVectorStore {
	if c != nil {
		c.client = client
	}
	return c
}

func (c *ChromaVectorStore) WithBaseURL(baseURL string) *ChromaVectorStore {
	if c != nil {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
	return c
}

func (c *ChromaVectorStore) WithCollection(name string) *ChromaVectorStore {
	if c != nil {
		c.collectionName = name
		c.collectionID = name
	}
	return c
}

func (c *ChromaVectorStore) Dimensions() int {
	if c == nil {
		return 0
	}
	return c.dimensions
}

func (c *ChromaVectorStore) ProviderName() string {
	return "chroma"
}

func (c *ChromaVectorStore) Close() error {
	return nil
}

func (c *ChromaVectorStore) AddVector(docID string, vec []float64) error {
	if c == nil {
		return fmt.Errorf("chroma vector store is nil")
	}
	return c.AddVectorBatch([]string{docID}, [][]float64{vec})
}

func (c *ChromaVectorStore) AddVectorBatch(docIDs []string, vectors [][]float64) error {
	if c == nil {
		return fmt.Errorf("chroma vector store is nil")
	}
	if len(docIDs) != len(vectors) {
		return fmt.Errorf("length mismatch: %d docIDs vs %d vectors", len(docIDs), len(vectors))
	}
	if len(docIDs) == 0 {
		return nil
	}

	for i, vec := range vectors {
		if len(vec) != c.dimensions {
			return fmt.Errorf("vector dimension mismatch at index %d: expected %d, got %d", i, c.dimensions, len(vec))
		}
	}

	payload := map[string]interface{}{
		"ids":        docIDs,
		"embeddings": vectors,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("%s/api/v1/collections/%s/add", c.baseURL, c.collectionID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("chroma add returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (c *ChromaVectorStore) DeleteVector(docID string) {
	if c == nil || docID == "" {
		return
	}

	payload := map[string]interface{}{
		"ids": []string{docID},
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return
	}

	apiURL := fmt.Sprintf("%s/api/v1/collections/%s/delete", c.baseURL, c.collectionID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	_ = resp.Body.Close()
}

type chromaQueryResponse struct {
	IDs       [][]string    `json:"ids"`
	Distances [][]float64  `json:"distances"`
}

func (c *ChromaVectorStore) SearchNearest(queryVector []float64, topK int) []VectorSearchResult {
	if c == nil || len(queryVector) != c.dimensions || topK <= 0 {
		return nil
	}

	payload := map[string]interface{}{
		"query_embeddings": [][]float64{queryVector},
		"n_results":        topK,
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	apiURL := fmt.Sprintf("%s/api/v1/collections/%s/query", c.baseURL, c.collectionID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var res chromaQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil
	}

	if len(res.IDs) == 0 || len(res.IDs[0]) == 0 {
		return []VectorSearchResult{}
	}

	var hits []VectorSearchResult
	ids := res.IDs[0]
	var dists []float64
	if len(res.Distances) > 0 {
		dists = res.Distances[0]
	}

	for i, id := range ids {
		similarity := 1.0
		if i < len(dists) {
			// Convert distance to cosine similarity (1.0 - distance)
			similarity = math.Max(0.0, 1.0-dists[i])
		}
		hits = append(hits, VectorSearchResult{
			DocID:      id,
			Similarity: math.Round(similarity*10000) / 10000,
		})
	}

	return hits
}

// -----------------------------------------------------------------------------
// PgVector Vector Store Adapter
// -----------------------------------------------------------------------------

// PgVectorStore implements VectorStore backed by PostgreSQL with pgvector.
type PgVectorStore struct {
	mu         sync.RWMutex
	db         *sql.DB
	tableName  string
	dimensions int
	dsn        string
}

// NewPgVectorStore creates a PgVectorStore from environment configuration.
func NewPgVectorStore(dim int) *PgVectorStore {
	if dim <= 0 {
		dim = 128
	}
	dsn := os.Getenv("PGVECTOR_DSN")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = os.Getenv("POSTGRES_URL")
	}

	tableName := os.Getenv("PGVECTOR_TABLE")
	if tableName == "" {
		tableName = "vector_documents"
	}

	var db *sql.DB
	if dsn != "" {
		var err error
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			logger.Log.Warn("Failed to open pgvector database connection", zap.Error(err))
		}
	}

	return &PgVectorStore{
		db:         db,
		tableName:  tableName,
		dimensions: dim,
		dsn:        dsn,
	}
}

// NewPgVectorStoreWithDB initializes PgVectorStore with an existing *sql.DB handle.
func NewPgVectorStoreWithDB(db *sql.DB, tableName string, dim int) *PgVectorStore {
	if dim <= 0 {
		dim = 128
	}
	if tableName == "" {
		tableName = "vector_documents"
	}
	return &PgVectorStore{
		db:         db,
		tableName:  tableName,
		dimensions: dim,
	}
}

func (p *PgVectorStore) Dimensions() int {
	if p == nil {
		return 0
	}
	return p.dimensions
}

func (p *PgVectorStore) ProviderName() string {
	return "pgvector"
}

func (p *PgVectorStore) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	err := p.db.Close()
	p.db = nil
	return err
}

func formatVectorForPg(vec []float64) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", v))
	}
	sb.WriteString("]")
	return sb.String()
}

func (p *PgVectorStore) AddVector(docID string, vec []float64) error {
	if p == nil {
		return fmt.Errorf("pgvector store is nil")
	}
	return p.AddVectorBatch([]string{docID}, [][]float64{vec})
}

func (p *PgVectorStore) AddVectorBatch(docIDs []string, vectors [][]float64) error {
	if p == nil {
		return fmt.Errorf("pgvector store is nil")
	}
	if len(docIDs) != len(vectors) {
		return fmt.Errorf("length mismatch: %d docIDs vs %d vectors", len(docIDs), len(vectors))
	}
	if len(docIDs) == 0 {
		return nil
	}

	for i, vec := range vectors {
		if len(vec) != p.dimensions {
			return fmt.Errorf("vector dimension mismatch at index %d: expected %d, got %d", i, p.dimensions, len(vec))
		}
	}

	p.mu.RLock()
	db := p.db
	p.mu.RUnlock()

	if db == nil {
		return fmt.Errorf("pgvector database connection is not initialized")
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (doc_id, embedding) VALUES ($1, $2) ON CONFLICT (doc_id) DO UPDATE SET embedding = EXCLUDED.embedding",
		p.tableName,
	)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, docID := range docIDs {
		vecStr := formatVectorForPg(vectors[i])
		if _, err := stmt.Exec(docID, vecStr); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (p *PgVectorStore) DeleteVector(docID string) {
	if p == nil || docID == "" {
		return
	}

	p.mu.RLock()
	db := p.db
	p.mu.RUnlock()

	if db == nil {
		return
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE doc_id = $1", p.tableName)
	_, _ = db.Exec(query, docID)
}

func (p *PgVectorStore) SearchNearest(queryVector []float64, topK int) []VectorSearchResult {
	if p == nil || len(queryVector) != p.dimensions || topK <= 0 {
		return nil
	}

	p.mu.RLock()
	db := p.db
	p.mu.RUnlock()

	if db == nil {
		return nil
	}

	queryStr := fmt.Sprintf(
		"SELECT doc_id, 1 - (embedding <=> $1) AS similarity FROM %s ORDER BY embedding <=> $1 LIMIT $2",
		p.tableName,
	)

	vecStr := formatVectorForPg(queryVector)
	rows, err := db.Query(queryStr, vecStr, topK)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var docID string
		var sim float64
		if err := rows.Scan(&docID, &sim); err == nil {
			results = append(results, VectorSearchResult{
				DocID:      docID,
				Similarity: math.Round(sim*10000) / 10000,
			})
		}
	}

	return results
}

// -----------------------------------------------------------------------------
// VectorStore Factory Function
// -----------------------------------------------------------------------------

// NewVectorStoreFromEnv returns a VectorStore implementation configured by the
// VECTOR_STORE_PROVIDER or VECTOR_STORE environment variable.
func NewVectorStoreFromEnv(dim int) VectorStore {
	if dim <= 0 {
		dim = 128
	}

	provider := strings.ToLower(os.Getenv("VECTOR_STORE_PROVIDER"))
	if provider == "" {
		provider = strings.ToLower(os.Getenv("VECTOR_STORE"))
	}

	switch provider {
	case "qdrant":
		logger.Log.Info("Initialized Qdrant Vector Store provider", zap.Int("dim", dim))
		return NewQdrantVectorStore(dim)
	case "chroma":
		logger.Log.Info("Initialized Chroma Vector Store provider", zap.Int("dim", dim))
		return NewChromaVectorStore(dim)
	case "pgvector", "postgres", "postgresql":
		logger.Log.Info("Initialized PgVector Vector Store provider", zap.Int("dim", dim))
		return NewPgVectorStore(dim)
	default:
		precisionStr := strings.ToLower(strings.TrimSpace(os.Getenv("VECTOR_PRECISION")))
		precision := PrecisionFloat32
		if precisionStr == "int8" {
			precision = PrecisionInt8
		} else if precisionStr == "float64" {
			precision = PrecisionFloat64
		}
		logger.Log.Info("Initialized In-Memory Vector Store", zap.Int("dim", dim), zap.String("precision", string(precision)))
		return NewVectorIndexWithPrecision(dim, precision)
	}
}
