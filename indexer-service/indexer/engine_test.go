package indexer

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentStressReadWriteRace(t *testing.T) {
	engine := NewIndexEngine()

	var wg sync.WaitGroup
	numWriters := 50
	numReaders := 50

	// Concurrent Writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			url := fmt.Sprintf("https://example.com/page/%d", id)
			title := fmt.Sprintf("Page Title %d", id)
			body := fmt.Sprintf("Clean body content for page %d with go programming language keywords", id)
			positions := map[string][]int{
				"go":          {1, 5},
				"programming": {2},
				"language":    {3},
			}
			engine.IndexDocument(url, title, body, positions, 10)
		}(i)
	}

	// Concurrent Readers calling GetMetadataMaps under race detector
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			titles, urls, bodies := engine.GetMetadataMaps()
			if titles == nil || urls == nil || bodies == nil {
				t.Errorf("GetMetadataMaps returned nil map")
			}
			docID := fmt.Sprintf("https://example.com/page/%d", id%numWriters)
			_, _, _, _ = engine.GetDocumentMetadata(docID)
		}(i)
	}

	wg.Wait()
}
