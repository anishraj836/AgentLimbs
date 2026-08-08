package index

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/internal/storage"
)

// Inverted Index

type PostingEntry struct {
	DocID         string `json:"doc_id"`
	TermFrequency int    `json:"term_frequency"`
	Positions     []int  `json:"positions"`
}

type PostingList struct {
	Term              string         `json:"term"`
	DocumentFrequency int            `json:"document_frequency"`
	Entries           []PostingEntry `json:"entries"`
}

type InvertedIndex struct {
	mu             sync.RWMutex
	postings       map[string]*PostingList
	docLengths     map[string]int
	totalDocLength int64
	totalDocuments int64
}

func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		postings:   make(map[string]*PostingList),
		docLengths: make(map[string]int),
	}
}

func (idx *InvertedIndex) AddDocument(docID string, termPositions map[string][]int, docLength int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	if oldLen, exists := idx.docLengths[docID]; exists {
		idx.totalDocLength -= int64(oldLen)
	} else {
		idx.totalDocuments++
	}
	idx.docLengths[docID] = docLength
	idx.totalDocLength += int64(docLength)

	for term, positions := range termPositions {
		pl, exists := idx.postings[term]
		if !exists {
			pl = &PostingList{
				Term:    term,
				Entries: make([]PostingEntry, 0),
			}
			idx.postings[term] = pl
		}

		found := false
		for i, entry := range pl.Entries {
			if entry.DocID == docID {
				pl.Entries[i].TermFrequency = len(positions)
				pl.Entries[i].Positions = positions
				found = true
				break
			}
		}

		if !found {
			pl.Entries = append(pl.Entries, PostingEntry{
				DocID:         docID,
				TermFrequency: len(positions),
				Positions:     positions,
			})
			pl.DocumentFrequency = len(pl.Entries)
		}
	}
}

func (idx *InvertedIndex) GetPostingList(term string) (*PostingList, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	pl, exists := idx.postings[term]
	if !exists {
		return nil, false
	}
	cp := &PostingList{
		Term:              pl.Term,
		DocumentFrequency: pl.DocumentFrequency,
		Entries:           make([]PostingEntry, len(pl.Entries)),
	}
	copy(cp.Entries, pl.Entries)
	return cp, true
}

func (idx *InvertedIndex) GetDocLength(docID string) int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docLengths[docID]
}

func (idx *InvertedIndex) GetStats() (totalDocs int64, avgDocLength float64, vocabularySize int) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	totalDocs = idx.totalDocuments
	if totalDocs > 0 {
		avgDocLength = float64(idx.totalDocLength) / float64(totalDocs)
	}
	vocabularySize = len(idx.postings)
	return
}

type indexSnapshot struct {
	Postings       map[string]*PostingList `json:"postings"`
	DocLengths     map[string]int          `json:"doc_lengths"`
	TotalDocLength int64                   `json:"total_doc_length"`
	TotalDocuments int64                   `json:"total_documents"`
}

func (idx *InvertedIndex) SaveSnapshot(filePath string) error {
	idx.mu.RLock()
	snap := indexSnapshot{
		Postings:       idx.postings,
		DocLengths:     idx.docLengths,
		TotalDocLength: idx.totalDocLength,
		TotalDocuments: idx.totalDocuments,
	}
	data, err := json.Marshal(snap)
	idx.mu.RUnlock()

	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (idx *InvertedIndex) LoadSnapshot(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var snap indexSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.postings = snap.Postings
	idx.docLengths = snap.DocLengths
	idx.totalDocLength = snap.TotalDocLength
	idx.totalDocuments = snap.TotalDocuments
	return nil
}

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

// Subword N-Gram Vector Store

type VectorSearchResult struct {
	DocID      string  `json:"doc_id"`
	Similarity float64 `json:"similarity"`
}

func CosineSimilarity(u, v []float64) float64 {
	if len(u) == 0 || len(v) == 0 || len(u) != len(v) {
		return 0.0
	}

	var dotProduct float64
	var normU float64
	var normV float64

	for i := 0; i < len(u); i++ {
		dotProduct += u[i] * v[i]
		normU += u[i] * u[i]
		normV += v[i] * v[i]
	}

	if normU == 0 || normV == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normU) * math.Sqrt(normV))
}

func hashFNV1a(s string, seed uint64) uint64 {
	h := seed
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func GenerateFeatureVector(text string, dimensions int) []float64 {
	if dimensions <= 0 {
		dimensions = 128
	}

	vec := make([]float64, dimensions)
	rawWords := strings.Fields(strings.ToLower(text))
	if len(rawWords) == 0 {
		return vec
	}

	var features []string
	for _, raw := range rawWords {
		w := strings.Trim(raw, ".,!?:;\"'()[]{}<>-")
		if w == "" || stopwords.IsStopword(w) {
			continue
		}

		features = append(features, w)

		runes := []rune(w)
		n := len(runes)
		for i := 0; i <= n-3; i++ {
			features = append(features, string(runes[i:i+3]))
		}
		for i := 0; i <= n-4; i++ {
			features = append(features, string(runes[i:i+4]))
		}
	}

	if len(features) == 0 {
		return vec
	}

	for _, f := range features {
		h1 := hashFNV1a(f, 14695981039346656037)
		h2 := hashFNV1a(f, 0xcbf29ce484222325)

		idx := int(h1 % uint64(dimensions))
		sign := 1.0
		if (h2 & 1) != 0 {
			sign = -1.0
		}
		vec[idx] += sign
	}

	var norm float64
	for _, val := range vec {
		norm += val * val
	}

	if norm > 0 {
		mag := math.Sqrt(norm)
		for i := 0; i < len(vec); i++ {
			vec[i] /= mag
		}
	}

	return vec
}

type VectorIndex struct {
	mu         sync.RWMutex
	dimensions int
	vectors    map[string][]float64
}

func NewVectorIndex(dimensions int) *VectorIndex {
	if dimensions <= 0 {
		dimensions = 128
	}
	return &VectorIndex{
		dimensions: dimensions,
		vectors:    make(map[string][]float64),
	}
}

func (vi *VectorIndex) AddVector(docID string, vec []float64) error {
	if len(vec) != vi.dimensions {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", vi.dimensions, len(vec))
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.vectors[docID] = vec
	return nil
}

func (vi *VectorIndex) SearchNearest(queryVector []float64, topK int) []VectorSearchResult {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if len(queryVector) != vi.dimensions || topK <= 0 {
		return nil
	}

	var results []VectorSearchResult
	for docID, vec := range vi.vectors {
		sim := CosineSimilarity(queryVector, vec)
		if sim > 0 {
			results = append(results, VectorSearchResult{
				DocID:      docID,
				Similarity: math.Round(sim*10000) / 10000,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if len(results) > topK {
		results = results[:topK]
	}

	return results
}

type vectorSnapshot struct {
	Dimensions int                  `json:"dimensions"`
	Vectors    map[string][]float64 `json:"vectors"`
}

func (vi *VectorIndex) SaveSnapshot(filePath string) error {
	vi.mu.RLock()
	snap := vectorSnapshot{
		Dimensions: vi.dimensions,
		Vectors:    vi.vectors,
	}
	data, err := json.Marshal(snap)
	vi.mu.RUnlock()

	if err != nil {
		return err
	}
	return os.WriteFile(filePath, data, 0644)
}

func (vi *VectorIndex) LoadSnapshot(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var snap vectorSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.dimensions = snap.Dimensions
	vi.vectors = snap.Vectors
	return nil
}

// BM25 Ranking Calculations

const (
	k1 = 1.2
	b  = 0.75
)

type SearchHit struct {
	DocID      string  `json:"doc_id"`
	Score      float64 `json:"score"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Snippet    string  `json:"snippet"`
	MatchCount int     `json:"match_count"`
}

func ComputeIDF(totalDocs int64, docFreq int) float64 {
	if totalDocs <= 0 || docFreq <= 0 {
		return 0.0
	}
	N := float64(totalDocs)
	n := float64(docFreq)
	num := N - n + 0.5
	den := n + 0.5
	if num < 0 {
		num = 0.5
	}
	return math.Log((num / den) + 1.0)
}

func ComputeBM25Score(tf int, docLen int, avgDocLen float64, idf float64) float64 {
	if tf <= 0 || idf <= 0 {
		return 0.0
	}
	if avgDocLen <= 0 {
		avgDocLen = 1.0
	}
	lenNorm := 1.0 - b + b*(float64(docLen)/avgDocLen)
	num := float64(tf) * (k1 + 1.0)
	den := float64(tf) + k1*lenNorm
	return idf * (num / den)
}

func RankDocuments(
	query string,
	invIndex *InvertedIndex,
	docTitles map[string]string,
	docURLs map[string]string,
	docBodies map[string]string,
	topK int,
) []SearchHit {
	if strings.TrimSpace(query) == "" {
		return nil
	}

	rawTokens := strings.Fields(strings.ToLower(query))
	var stemmedTokens []string
	var queryTerms []string

	for _, t := range rawTokens {
		t = strings.Trim(t, ".,!?:;\"'()[]{}")
		if t == "" || stopwords.IsStopword(t) {
			continue
		}
		stemmed := stemmer.Stem(t)
		stemmedTokens = append(stemmedTokens, stemmed)
		queryTerms = append(queryTerms, t)
	}

	if len(stemmedTokens) == 0 {
		return nil
	}

	totalDocs, avgDocLen, _ := invIndex.GetStats()
	if totalDocs == 0 {
		return nil
	}

	docScores := make(map[string]float64)
	docMatchCounts := make(map[string]int)

	for _, term := range stemmedTokens {
		pl, exists := invIndex.GetPostingList(term)
		if !exists {
			continue
		}

		idf := ComputeIDF(totalDocs, pl.DocumentFrequency)
		for _, entry := range pl.Entries {
			docLen := invIndex.GetDocLength(entry.DocID)
			score := ComputeBM25Score(entry.TermFrequency, docLen, avgDocLen, idf)
			docScores[entry.DocID] += score
			docMatchCounts[entry.DocID]++
		}
	}

	var hits []SearchHit
	for docID, score := range docScores {
		body := docBodies[docID]
		snippet := GenerateHighlightedSnippet(body, queryTerms, 180)

		title := docTitles[docID]
		if title == "" {
			title = docID
		}
		url := docURLs[docID]

		hits = append(hits, SearchHit{
			DocID:      docID,
			Score:      math.Round(score*1000) / 1000,
			Title:      title,
			URL:        url,
			Snippet:    snippet,
			MatchCount: docMatchCounts[docID],
		})
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if len(hits) > topK {
		hits = hits[:topK]
	}

	return hits
}

func GenerateHighlightedSnippet(body string, queryTerms []string, maxLen int) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}

	words := strings.Fields(body)
	if len(words) == 0 {
		return ""
	}

	matchIdx := 0
	lowerBody := strings.ToLower(body)
	firstMatchPos := len(lowerBody)

	for _, q := range queryTerms {
		pos := strings.Index(lowerBody, strings.ToLower(q))
		if pos != -1 && pos < firstMatchPos {
			firstMatchPos = pos
		}
	}

	if firstMatchPos != len(lowerBody) {
		runningLen := 0
		for i, w := range words {
			runningLen += len(w) + 1
			if runningLen >= firstMatchPos {
				matchIdx = i
				break
			}
		}
	}

	startIdx := matchIdx - 5
	if startIdx < 0 {
		startIdx = 0
	}
	endIdx := startIdx + 25
	if endIdx > len(words) {
		endIdx = len(words)
	}

	snippetWords := make([]string, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		w := words[i]
		cleanW := strings.Trim(strings.ToLower(w), ".,!?:;\"'()[]{}")
		matched := false
		for _, q := range queryTerms {
			if cleanW == strings.ToLower(q) || stemmer.Stem(cleanW) == stemmer.Stem(q) {
				matched = true
				break
			}
		}

		if matched {
			snippetWords = append(snippetWords, fmt.Sprintf("<mark>%s</mark>", w))
		} else {
			snippetWords = append(snippetWords, w)
		}
	}

	result := strings.Join(snippetWords, " ")
	if startIdx > 0 {
		result = "..." + result
	}
	if endIdx < len(words) {
		result = result + "..."
	}

	runes := []rune(result)
	if len(runes) > maxLen {
		result = string(runes[:maxLen]) + "..."
	}

	return result
}

// Index Engine Component

type Engine struct {
	mu             sync.RWMutex
	Inverted       *InvertedIndex
	Trie           *Trie
	Vector         *VectorIndex
	ActiveEmbedder Embedder
	DocTitles      map[string]string
	DocURLs        map[string]string
	DocBodies      map[string]string
}

type IndexEngine = Engine

var GlobalEngine = NewEngine()

func NewEngine() *Engine {
	active := NewEmbedderFromEnv()
	return &Engine{
		Inverted:       NewInvertedIndex(),
		Trie:           NewTrie(),
		Vector:         NewVectorIndex(active.Dimensions()),
		ActiveEmbedder: active,
		DocTitles:      make(map[string]string),
		DocURLs:        make(map[string]string),
		DocBodies:      make(map[string]string),
	}
}

func NewIndexEngine() *Engine {
	return NewEngine()
}

func (e *Engine) IndexDocument(url, title, cleanBody string, termPositions map[string][]int, totalTokens int) {
	e.IndexDocumentWithSource(url, title, cleanBody, termPositions, totalTokens, "web_crawled", url)
}

func (e *Engine) IndexDocumentWithSource(url, title, cleanBody string, termPositions map[string][]int, totalTokens int, sourceType, sourceURL string) {
	docID := url

	e.mu.Lock()
	e.DocTitles[docID] = title
	e.DocURLs[docID] = url
	e.DocBodies[docID] = cleanBody
	e.mu.Unlock()

	e.Inverted.AddDocument(docID, termPositions, totalTokens)
	for term, positions := range termPositions {
		e.Trie.Insert(term, len(positions))
	}
	e.IndexDocumentVector(docID, title, cleanBody)

	_ = storage.SaveCrawledDocument(context.Background(), url, title, cleanBody, totalTokens, sourceType, sourceURL)
}

func (e *Engine) IndexDocumentVector(docID, title, body string) {
	vec, err := e.ActiveEmbedder.Embed(context.Background(), title+" "+body)
	if err != nil || len(vec) == 0 {
		return
	}
	_ = e.Vector.AddVector(docID, vec)
}

func (e *Engine) LoadFromDB(ctx context.Context) error {
	docs, err := storage.GetCrawledDocuments(ctx)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.DocTitles = make(map[string]string)
	e.DocURLs = make(map[string]string)
	e.DocBodies = make(map[string]string)
	e.Inverted = NewInvertedIndex()
	e.Trie = NewTrie()
	e.Vector = NewVectorIndex(e.ActiveEmbedder.Dimensions())
	e.mu.Unlock()

	for _, d := range docs {
		// Helper tokenization for DB hydration
		rawTokens := strings.Fields(strings.ToLower(d.CleanBody))
		termPositions := make(map[string][]int)
		for idx, raw := range rawTokens {
			clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
			if clean == "" || stopwords.IsStopword(clean) {
				continue
			}
			stemmed := stemmer.Stem(clean)
			termPositions[stemmed] = append(termPositions[stemmed], idx)
		}

		e.mu.Lock()
		e.DocTitles[d.URL] = d.Title
		e.DocURLs[d.URL] = d.URL
		e.DocBodies[d.URL] = d.CleanBody
		e.mu.Unlock()

		e.Inverted.AddDocument(d.URL, termPositions, d.TotalTokens)
		for term, positions := range termPositions {
			e.Trie.Insert(term, len(positions))
		}
		e.IndexDocumentVector(d.URL, d.Title, d.CleanBody)
	}
	return nil
}

func (e *Engine) StartTTLJanitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				_, _ = storage.DeleteExpiredDocuments(ctx)
				_ = e.LoadFromDB(ctx)
			}
		}
	}()
}

func (e *Engine) StartPeriodicDBHydrator(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				_ = e.LoadFromDB(ctx)
			}
		}
	}()
}

func (e *Engine) GetDocumentMetadata(docID string) (title, url, body string, exists bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	title, exists = e.DocTitles[docID]
	url = e.DocURLs[docID]
	body = e.DocBodies[docID]
	return
}

func (e *Engine) GetMetadataMaps() (titles, urls, bodies map[string]string) {
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

func (e *Engine) SaveSnapshot(dataDir string) error {
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	if err := e.Inverted.SaveSnapshot(indexPath); err != nil {
		return err
	}

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	if err := e.Vector.SaveSnapshot(vectorPath); err != nil {
		return err
	}

	return nil
}

func (e *Engine) LoadSnapshot(dataDir string) error {
	if dataDir == "" {
		dataDir = "data"
	}

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	if _, err := os.Stat(indexPath); err == nil {
		if err := e.Inverted.LoadSnapshot(indexPath); err != nil {
			return err
		}
	}

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	if _, err := os.Stat(vectorPath); err == nil {
		if err := e.Vector.LoadSnapshot(vectorPath); err != nil {
			return err
		}
	}

	return nil
}

func (e *Engine) SearchBM25(query string, topK int) []SearchHit {
	titles, urls, bodies := e.GetMetadataMaps()
	return RankDocuments(query, e.Inverted, titles, urls, bodies, topK)
}

func (e *Engine) SearchVector(query string, topK int) []VectorSearchResult {
	queryVec := GenerateFeatureVector(query, 128)
	return e.Vector.SearchNearest(queryVec, topK)
}
