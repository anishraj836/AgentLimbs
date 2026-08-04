package storage

import (
	"bytes"
	"compress/gzip"
	"github.com/crawler-monorepo/common/utils"
	"io"
	"os"
	"path/filepath"
	"time"
)

type LocalStorage struct {
	baseDir string
}

func NewLocalStorage(baseDir string) *LocalStorage {
	return &LocalStorage{baseDir: baseDir}
}

// Save compresses and stores raw HTML on the filesystem, partitioning by Date and Domain.
// If any write step fails, the partial file is removed to prevent corrupt artifacts.
func (s *LocalStorage) Save(url string, content []byte) (string, error) {
	domain, err := utils.GetDomain(url)
	if err != nil {
		return "", err
	}

	// Sanitize domain directory name to prevent path traversal or illegal characters
	safeDomain := filepath.Base(filepath.Clean(domain))
	if safeDomain == "." || safeDomain == "/" || safeDomain == "" {
		safeDomain = "unknown"
	}

	hash := utils.HashURL(url)
	dateStr := time.Now().Format("2006-01-02")

	dirPath := filepath.Join(s.baseDir, dateStr, safeDomain)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", err
	}

	filePath := filepath.Join(dirPath, hash+".html.gz")

	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}

	// Use explicit close (not defer) so we can detect gzip flush errors
	// and clean up the corrupt file on any failure.
	gw := gzip.NewWriter(file)

	_, copyErr := io.Copy(gw, bytes.NewReader(content))

	// Close gzip writer first — this flushes the gzip trailer.
	// If io.Copy failed, gw.Close() may also fail; capture whichever error came first.
	gwCloseErr := gw.Close()
	fileCloseErr := file.Close()

	// If anything failed, remove the partial/corrupt file
	if copyErr != nil || gwCloseErr != nil || fileCloseErr != nil {
		os.Remove(filePath)
		if copyErr != nil {
			return "", copyErr
		}
		if gwCloseErr != nil {
			return "", gwCloseErr
		}
		return "", fileCloseErr
	}

	return filePath, nil
}
