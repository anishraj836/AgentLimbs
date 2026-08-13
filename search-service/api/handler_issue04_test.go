package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crawler-monorepo/internal/index"
)

func TestSearchPaginationOverflow_Issue04(t *testing.T) {
	searchEngine := index.NewEngine()
	handler := NewSearchHandler(searchEngine)

	reqBody := `{"query":"test","offset":9223372036854775800,"limit":10}`
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Search(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request from search-service, got %d", resp.StatusCode)
	}
}
