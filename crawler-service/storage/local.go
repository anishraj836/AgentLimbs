package storage

import (
	"github.com/crawler-monorepo/internal/storage"
)

type LocalStorage = storage.LocalStorage

func NewLocalStorage(baseDir string) *LocalStorage {
	return storage.NewLocalStorage(baseDir)
}
