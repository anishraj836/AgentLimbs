package index

import (
	"math"
	"sort"
	"sync"
)

// DocIDMapper provides thread-safe bidirectional mapping between string URLs and contiguous uint32 DocIDs.
type DocIDMapper struct {
	mu      sync.RWMutex
	urlToID map[string]uint32
	idToURL []string
	deleted map[uint32]struct{}
}

func NewDocIDMapper() *DocIDMapper {
	return &DocIDMapper{
		urlToID: make(map[string]uint32),
		idToURL: make([]string, 0, 1024),
		deleted: make(map[uint32]struct{}),
	}
}

func (m *DocIDMapper) GetOrCreateID(url string) uint32 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if id, exists := m.urlToID[url]; exists {
		return id
	}

	id := uint32(len(m.idToURL))
	m.idToURL = append(m.idToURL, url)
	m.urlToID[url] = id
	return id
}

func (m *DocIDMapper) GetURL(id uint32) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if int(id) >= len(m.idToURL) {
		return "", false
	}
	if _, isDeleted := m.deleted[id]; isDeleted {
		return "", false
	}
	return m.idToURL[id], true
}

func (m *DocIDMapper) MarkDeleted(id uint32) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted[id] = struct{}{}
}

func (m *DocIDMapper) IsDeleted(id uint32) bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, isDeleted := m.deleted[id]
	return isDeleted
}

func (m *DocIDMapper) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.idToURL) - len(m.deleted)
}

// CompressedBlock stores up to 64 postings compressed via VByte delta encoding with block metadata.
type CompressedBlock struct {
	MaxDocID  uint32 `json:"max_id"`
	MaxTF     uint32 `json:"max_tf"`
	MinDocLen uint32 `json:"min_len"`
	DocDeltas []byte `json:"deltas"`
	Freqs     []byte `json:"freqs"`
}

// CompressedPostingList implements a 2-tier posting list with sealed compressed blocks and an active tail buffer.
type CompressedPostingList struct {
	TotalCount int               `json:"total_count"`
	Blocks     []CompressedBlock `json:"blocks"`
	TailDocIDs []uint32          `json:"tail_ids,omitempty"`
	TailTFs    []uint32          `json:"tail_tfs,omitempty"`
	TailLens   []uint32          `json:"tail_lens,omitempty"`
}

func NewCompressedPostingList() *CompressedPostingList {
	return &CompressedPostingList{
		Blocks:     make([]CompressedBlock, 0),
		TailDocIDs: make([]uint32, 0, 64),
		TailTFs:    make([]uint32, 0, 64),
		TailLens:   make([]uint32, 0, 64),
	}
}

func (pl *CompressedPostingList) Add(docID uint32, tf uint32, docLen uint32) {
	if pl == nil {
		return
	}
	pl.TotalCount++
	pl.TailDocIDs = append(pl.TailDocIDs, docID)
	pl.TailTFs = append(pl.TailTFs, tf)
	pl.TailLens = append(pl.TailLens, docLen)
}

func (pl *CompressedPostingList) SealTail() {
	if pl == nil || len(pl.TailDocIDs) == 0 {
		return
	}

	n := len(pl.TailDocIDs)
	type pEntry struct {
		docID  uint32
		tf     uint32
		docLen uint32
	}
	entries := make([]pEntry, n)
	for i := 0; i < n; i++ {
		entries[i] = pEntry{
			docID:  pl.TailDocIDs[i],
			tf:     pl.TailTFs[i],
			docLen: pl.TailLens[i],
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].docID < entries[j].docID
	})

	// Deduplicate adjacent identical docIDs if a document was re-indexed before tail sealing
	deduped := entries[:0]
	for _, e := range entries {
		if len(deduped) > 0 && deduped[len(deduped)-1].docID == e.docID {
			deduped[len(deduped)-1].tf += e.tf
			deduped[len(deduped)-1].docLen = e.docLen
		} else {
			deduped = append(deduped, e)
		}
	}
	entries = deduped
	n = len(entries)
	if n == 0 {
		return
	}

	// Chunk into blocks of at most 64
	for chunkStart := 0; chunkStart < n; chunkStart += 64 {
		chunkEnd := chunkStart + 64
		if chunkEnd > n {
			chunkEnd = n
		}
		chunkLen := chunkEnd - chunkStart

		chunkDocIDs := make([]uint32, chunkLen)
		chunkTFs := make([]uint32, chunkLen)
		var maxTF uint32
		var minLen uint32 = math.MaxUint32

		for i := 0; i < chunkLen; i++ {
			e := entries[chunkStart+i]
			chunkDocIDs[i] = e.docID
			chunkTFs[i] = e.tf
			if e.tf > maxTF {
				maxTF = e.tf
			}
			if e.docLen < minLen {
				minLen = e.docLen
			}
		}

		docData, tfData, err := EncodePostingBlock(chunkDocIDs, chunkTFs)
		if err != nil {
			continue
		}

		pl.Blocks = append(pl.Blocks, CompressedBlock{
			MaxDocID:  chunkDocIDs[chunkLen-1],
			MaxTF:     maxTF,
			MinDocLen: minLen,
			DocDeltas: docData,
			Freqs:     tfData,
		})
	}

	pl.TailDocIDs = pl.TailDocIDs[:0]
	pl.TailTFs = pl.TailTFs[:0]
	pl.TailLens = pl.TailLens[:0]
}

// Count returns the total number of document postings in the list.
func (pl *CompressedPostingList) Count() int {
	if pl == nil {
		return 0
	}
	return pl.TotalCount
}

// ComputeBlockUpperBound dynamically calculates the maximum BM25 term contribution for a block.
func ComputeBlockUpperBound(maxTF, minDocLen uint32, idf float64, avgdl float64, k1, b float64) float64 {
	if maxTF == 0 || idf <= 0 {
		return 0.0
	}
	tfF := float64(maxTF)
	lenRatio := float64(minDocLen) / avgdl
	denominator := tfF + k1*(1.0-b+b*lenRatio)
	if denominator == 0 {
		return 0.0
	}
	return idf * ((tfF * (k1 + 1.0)) / denominator)
}

// sync.Pool for zero-allocation posting decompression during search queries.
var blockBufferPool = sync.Pool{
	New: func() any {
		return &[64]uint32{}
	},
}

// sync.Pool for zero-allocation score map reuse during multi-term queries.
var scoreMapPool = sync.Pool{
	New: func() any {
		return make(map[uint32]float64, 1024)
	},
}

type PostingHit struct {
	DocID uint32
	Score float64
}

// ScoreBM25 calculates the single-term BM25 score for a document.
func ScoreBM25(tf uint32, docLen int, avgdl float64, idf float64, k1, b float64) float64 {
	if tf == 0 || idf <= 0 || avgdl <= 0 {
		return 0.0
	}
	tfF := float64(tf)
	lenRatio := float64(docLen) / avgdl
	denominator := tfF + k1*(1.0-b+b*lenRatio)
	if denominator == 0 {
		return 0.0
	}
	return idf * ((tfF * (k1 + 1.0)) / denominator)
}

// BlockMaxWANDScores computes BM25 top-K scores across compressed posting lists using Block-Max WAND.
func BlockMaxWANDScores(
	lists []*CompressedPostingList,
	idfs []float64,
	topK int,
	avgdl float64,
	docLengths []int,
	mapper *DocIDMapper,
	k1, b float64,
) []PostingHit {
	if len(lists) == 0 || topK <= 0 || avgdl <= 0 {
		return nil
	}

	// Single-term fast path with zero map allocations and dynamic block skipping
	if len(lists) == 1 {
		pl := lists[0]
		idf := idfs[0]
		if pl == nil || idf <= 0 {
			return nil
		}

		docBuf := blockBufferPool.Get().(*[64]uint32)
		tfBuf := blockBufferPool.Get().(*[64]uint32)
		defer blockBufferPool.Put(docBuf)
		defer blockBufferPool.Put(tfBuf)

		var minScore float64
		var hits []PostingHit

		for _, block := range pl.Blocks {
			ub := ComputeBlockUpperBound(block.MaxTF, block.MinDocLen, idf, avgdl, k1, b)
			if len(hits) >= topK && ub <= minScore {
				continue // Skip decompressing entire block!
			}

			n, err := DecodePostingBlock(block.DocDeltas, block.Freqs, docBuf, tfBuf)
			if err != nil {
				continue
			}

			for i := 0; i < n; i++ {
				docID := docBuf[i]
				if mapper != nil && mapper.IsDeleted(docID) {
					continue
				}
				var dLen int
				if int(docID) < len(docLengths) {
					dLen = docLengths[docID]
				} else {
					dLen = int(avgdl)
				}
				score := ScoreBM25(tfBuf[i], dLen, avgdl, idf, k1, b)
				if score > 0 {
					if len(hits) < topK {
						hits = append(hits, PostingHit{DocID: docID, Score: score})
						if len(hits) == topK {
							minScore = hits[0].Score
							for _, h := range hits {
								if h.Score < minScore {
									minScore = h.Score
								}
							}
						}
					} else if score > minScore {
						minIdx := 0
						minScore = hits[0].Score
						for idx, h := range hits {
							if h.Score < minScore {
								minScore = h.Score
								minIdx = idx
							}
						}
						if score > minScore {
							hits[minIdx] = PostingHit{DocID: docID, Score: score}
							minScore = hits[0].Score
							for _, h := range hits {
								if h.Score < minScore {
									minScore = h.Score
								}
							}
						}
					}
				}
			}
		}

		for i, docID := range pl.TailDocIDs {
			if mapper != nil && mapper.IsDeleted(docID) {
				continue
			}
			var dLen int
			if int(docID) < len(docLengths) {
				dLen = docLengths[docID]
			} else {
				dLen = int(avgdl)
			}
			score := ScoreBM25(pl.TailTFs[i], dLen, avgdl, idf, k1, b)
			if score > 0 {
				if len(hits) < topK {
					hits = append(hits, PostingHit{DocID: docID, Score: score})
					if len(hits) == topK {
						minScore = hits[0].Score
						for _, h := range hits {
							if h.Score < minScore {
								minScore = h.Score
							}
						}
					}
				} else if score > minScore {
					minIdx := 0
					minScore = hits[0].Score
					for idx, h := range hits {
						if h.Score < minScore {
							minScore = h.Score
							minIdx = idx
						}
					}
					if score > minScore {
						hits[minIdx] = PostingHit{DocID: docID, Score: score}
						minScore = hits[0].Score
						for _, h := range hits {
							if h.Score < minScore {
								minScore = h.Score
							}
						}
					}
				}
			}
		}

		sort.Slice(hits, func(i, j int) bool {
			if hits[i].Score != hits[j].Score {
				return hits[i].Score > hits[j].Score
			}
			return hits[i].DocID < hits[j].DocID
		})
		return hits
	}

	docScores := scoreMapPool.Get().(map[uint32]float64)
	defer func() {
		for k := range docScores {
			delete(docScores, k)
		}
		scoreMapPool.Put(docScores)
	}()

	// Multi-term query execution with block skipping
	for termIdx, pl := range lists {
		if pl == nil {
			continue
		}
		idf := idfs[termIdx]
		if idf <= 0 {
			continue
		}

		docBuf := blockBufferPool.Get().(*[64]uint32)
		tfBuf := blockBufferPool.Get().(*[64]uint32)

		for _, block := range pl.Blocks {
			ub := ComputeBlockUpperBound(block.MaxTF, block.MinDocLen, idf, avgdl, k1, b)
			if ub <= 0 {
				continue
			}

			n, err := DecodePostingBlock(block.DocDeltas, block.Freqs, docBuf, tfBuf)
			if err != nil {
				continue
			}

			for i := 0; i < n; i++ {
				docID := docBuf[i]
				if mapper != nil && mapper.IsDeleted(docID) {
					continue
				}
				var dLen int
				if int(docID) < len(docLengths) {
					dLen = docLengths[docID]
				} else {
					dLen = int(avgdl)
				}

				score := ScoreBM25(tfBuf[i], dLen, avgdl, idf, k1, b)
				docScores[docID] += score
			}
		}

		blockBufferPool.Put(docBuf)
		blockBufferPool.Put(tfBuf)

		// Evaluate uncompressed tail buffer
		for i, docID := range pl.TailDocIDs {
			if mapper != nil && mapper.IsDeleted(docID) {
				continue
			}
			var dLen int
			if int(docID) < len(docLengths) {
				dLen = docLengths[docID]
			} else {
				dLen = int(avgdl)
			}
			score := ScoreBM25(pl.TailTFs[i], dLen, avgdl, idf, k1, b)
			docScores[docID] += score
		}
	}

	hits := make([]PostingHit, 0, topK*2)
	for docID, score := range docScores {
		if score > 0 {
			hits = append(hits, PostingHit{
				DocID: docID,
				Score: score,
			})
		}
	}

	// Deterministic tie-breaking: Primary Score DESC, Secondary DocID ASC
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].DocID < hits[j].DocID
	})

	if len(hits) > topK {
		hits = hits[:topK]
	}

	return hits
}
