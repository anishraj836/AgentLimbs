package index

import (
	"github.com/crawler-monorepo/internal/index"
)

type PostingEntry = index.PostingEntry
type PostingList = index.PostingList
type InvertedIndex = index.InvertedIndex

func NewInvertedIndex() *InvertedIndex {
	return index.NewInvertedIndex()
}
