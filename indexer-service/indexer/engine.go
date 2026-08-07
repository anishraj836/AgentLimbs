package indexer

import (
	"github.com/crawler-monorepo/internal/index"
)

type IndexEngine = index.Engine

var GlobalEngine = index.GlobalEngine

func NewIndexEngine() *IndexEngine {
	return index.NewEngine()
}
