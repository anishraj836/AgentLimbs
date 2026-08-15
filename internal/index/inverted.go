package index

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"
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
	docIndex          map[string]int
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

	if prevLen, exists := idx.docLengths[docID]; exists {
		idx.totalDocLength += int64(docLength - prevLen)
	} else {
		idx.totalDocuments++
		idx.totalDocLength += int64(docLength)
	}
	idx.docLengths[docID] = docLength

	for term, positions := range termPositions {
		pl, exists := idx.postings[term]
		if !exists {
			pl = &PostingList{
				Term:     term,
				Entries:  make([]PostingEntry, 0),
				docIndex: make(map[string]int),
			}
			idx.postings[term] = pl
		}

		if pl.docIndex == nil {
			pl.docIndex = make(map[string]int, len(pl.Entries))
			for i, entry := range pl.Entries {
				pl.docIndex[entry.DocID] = i
			}
		}

		if entryIdx, found := pl.docIndex[docID]; found {
			pl.Entries[entryIdx].TermFrequency = len(positions)
			pl.Entries[entryIdx].Positions = positions
		} else {
			pl.docIndex[docID] = len(pl.Entries)
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
	for i, entry := range pl.Entries {
		var posCopy []int
		if entry.Positions != nil {
			posCopy = make([]int, len(entry.Positions))
			copy(posCopy, entry.Positions)
		}
		cp.Entries[i] = PostingEntry{
			DocID:         entry.DocID,
			TermFrequency: entry.TermFrequency,
			Positions:     posCopy,
		}
	}
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
	docLengthsCopy := make(map[string]int, len(idx.docLengths))
	for k, v := range idx.docLengths {
		docLengthsCopy[k] = v
	}
	postingsCopy := make(map[string]*PostingList, len(idx.postings))
	for k, pl := range idx.postings {
		entriesCopy := make([]PostingEntry, len(pl.Entries))
		for i, entry := range pl.Entries {
			var posCopy []int
			if entry.Positions != nil {
				posCopy = make([]int, len(entry.Positions))
				copy(posCopy, entry.Positions)
			}
			entriesCopy[i] = PostingEntry{
				DocID:         entry.DocID,
				TermFrequency: entry.TermFrequency,
				Positions:     posCopy,
			}
		}
		postingsCopy[k] = &PostingList{
			Term:              pl.Term,
			DocumentFrequency: pl.DocumentFrequency,
			Entries:           entriesCopy,
		}
	}
	totalDocLength := idx.totalDocLength
	totalDocuments := idx.totalDocuments
	idx.mu.RUnlock()

	snap := indexSnapshot{
		Postings:       postingsCopy,
		DocLengths:     docLengthsCopy,
		TotalDocLength: totalDocLength,
		TotalDocuments: totalDocuments,
	}

	tmpPath := filePath + ".tmp"
	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if err := json.NewEncoder(tmpFile).Encode(snap); err != nil {
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	tmpFile = nil

	return os.Rename(tmpPath, filePath)
}

func (idx *InvertedIndex) LoadSnapshot(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	if len(bytes.TrimSpace(data)) == 0 {
		idx.mu.Lock()
		defer idx.mu.Unlock()
		idx.postings = make(map[string]*PostingList)
		idx.docLengths = make(map[string]int)
		idx.totalDocLength = 0
		idx.totalDocuments = 0
		return nil
	}

	var snap indexSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	if snap.Postings == nil {
		idx.postings = make(map[string]*PostingList)
	} else {
		idx.postings = snap.Postings
	}
	if snap.DocLengths == nil {
		idx.docLengths = make(map[string]int)
	} else {
		idx.docLengths = snap.DocLengths
	}
	idx.totalDocLength = snap.TotalDocLength
	idx.totalDocuments = snap.TotalDocuments
	return nil
}

// DeleteDocument removes a document from the inverted index postings and docLengths.
func (idx *InvertedIndex) DeleteDocument(docID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	length, exists := idx.docLengths[docID]
	if !exists {
		return
	}
	delete(idx.docLengths, docID)
	idx.totalDocuments--
	idx.totalDocLength -= int64(length)
	if idx.totalDocLength < 0 {
		idx.totalDocLength = 0
	}
	if idx.totalDocuments < 0 {
		idx.totalDocuments = 0
	}

	for term, pl := range idx.postings {
		if idxPos, found := pl.docIndex[docID]; found {
			delete(pl.docIndex, docID)
			pl.Entries = append(pl.Entries[:idxPos], pl.Entries[idxPos+1:]...)
			pl.DocumentFrequency = len(pl.Entries)
			for i, entry := range pl.Entries {
				pl.docIndex[entry.DocID] = i
			}
			if len(pl.Entries) == 0 {
				delete(idx.postings, term)
			}
		}
	}
}

