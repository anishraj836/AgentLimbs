package hybrid

import (
	"github.com/crawler-monorepo/common/bm25"
	"github.com/crawler-monorepo/common/vector"
	"github.com/crawler-monorepo/internal/search"
)

const RRFConstant = search.RRFConstant

type HybridSearchHit = search.HybridSearchHit

func ReciprocalRankFusion(
	bm25Hits []bm25.SearchHit,
	vectorHits []vector.VectorSearchResult,
	topK int,
) []HybridSearchHit {
	return search.ReciprocalRankFusion(bm25Hits, vectorHits, topK)
}
