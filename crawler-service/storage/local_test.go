package storage

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorageSave(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "storage_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ls := NewLocalStorage(tempDir)
	content := []byte("<html><body>Hello World</body></html>")
	targetURL := "https://EXAMPLE.COM:8080/path/to/page"

	savedPath, err := ls.Save(targetURL, content)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if !filepath.IsAbs(savedPath) {
		t.Errorf("expected absolute path, got %s", savedPath)
	}

	file, err := os.Open(savedPath)
	if err != nil {
		t.Fatalf("Failed to open saved file: %v", err)
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	readBytes, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("Failed to read decompressed bytes: %v", err)
	}

	if !bytes.Equal(readBytes, content) {
		t.Errorf("expected content %q, got %q", string(content), string(readBytes))
	}
}
