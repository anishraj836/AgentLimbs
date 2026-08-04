package index

import (
	"sync"
)

// PostingEntry stores occurrences of a term within a specific document.
type PostingEntry struct {
	DocID         string   `json:"doc_id"`
	TermFrequency int      `json:"term_frequency"`
	Positions     []int    `json:"positions"` // Word positions for phrase search
}

// PostingList stores document occurrences for a specific term.
type PostingList struct {
	Term              string         `json:"term"`
	DocumentFrequency int            `json:"document_frequency"`
	Entries           []PostingEntry `json:"entries"`
}

// InvertedIndex is a thread-safe in-memory posting list index with document statistics.
type InvertedIndex struct {
	mu             sync.RWMutex
	postings       map[string]*PostingList // term -> posting list
	docLengths     map[string]int          // docID -> docLength
	totalDocLength int64
	totalDocuments int64
}

func NewInvertedIndex() *InvertedIndex {
	return &InvertedIndex{
		postings:   make(map[string]*PostingList),
		docLengths: make(map[string]int),
	}
}

// AddDocument indexes a tokenized document with term frequencies and positions.
func (idx *InvertedIndex) AddDocument(docID string, termPositions map[string][]int, docLength int) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Update document length stats
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

		// Update or append posting entry
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

// GetPostingList retrieves the posting list for a given term.
func (idx *InvertedIndex) GetPostingList(term string) (*PostingList, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	pl, exists := idx.postings[term]
	if !exists {
		return nil, false
	}
	// Copy to avoid data race
	cp := &PostingList{
		Term:              pl.Term,
		DocumentFrequency: pl.DocumentFrequency,
		Entries:           make([]PostingEntry, len(pl.Entries)),
	}
	copy(cp.Entries, pl.Entries)
	return cp, true
}

// GetDocLength returns the token count for a document.
func (idx *InvertedIndex) GetDocLength(docID string) int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docLengths[docID]
}

// GetStats returns global corpus statistics for BM25 calculations.
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
