package index

import (
	"os"
	"path/filepath"
)

// Engine Snapshot Persistence

func (e *Engine) SaveSnapshot(dataDir string) error {
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)

	e.mu.RLock()
	inv := e.Inverted
	vec := e.Vector
	e.mu.RUnlock()

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	if err := inv.SaveSnapshot(indexPath); err != nil {
		return err
	}

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	if err := vec.SaveSnapshot(vectorPath); err != nil {
		return err
	}

	return nil
}

func (e *Engine) LoadSnapshot(dataDir string) error {
	if dataDir == "" {
		dataDir = "data"
	}

	e.mu.RLock()
	inv := e.Inverted
	vec := e.Vector
	e.mu.RUnlock()

	indexPath := filepath.Join(dataDir, "inverted_index.json")
	if _, err := os.Stat(indexPath); err == nil {
		if err := inv.LoadSnapshot(indexPath); err != nil {
			return err
		}
	}

	vectorPath := filepath.Join(dataDir, "vector_index.json")
	if _, err := os.Stat(vectorPath); err == nil {
		if err := vec.LoadSnapshot(vectorPath); err != nil {
			return err
		}
	}

	return nil
}
