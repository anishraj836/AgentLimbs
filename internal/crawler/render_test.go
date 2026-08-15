package crawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShouldAbortResource(t *testing.T) {
	engine := NewFallbackRenderEngine("chrome")

	mediaURLs := []string{
		"https://example.com/image.png",
		"https://example.com/photo.jpeg",
		"https://example.com/font.woff2",
		"https://example.com/video.mp4",
		"https://example.com/audio.mp3",
	}

	for _, u := range mediaURLs {
		if !engine.ShouldAbortResource(u) {
			t.Errorf("Expected ShouldAbortResource(%s) to be true", u)
		}
	}

	allowedURLs := []string{
		"https://example.com/app.js",
		"https://example.com/style.css",
		"https://example.com/page.html",
		"https://example.com/api/data.json",
	}

	for _, u := range allowedURLs {
		if engine.ShouldAbortResource(u) {
			t.Errorf("Expected ShouldAbortResource(%s) to be false", u)
		}
	}
}

func TestRenderSPA(t *testing.T) {
	ctx := context.Background()

	// Default engine
	engine := NewFallbackRenderEngine("chrome")
	html, err := engine.RenderSPA(ctx, "https://example.com/spa")
	if err != nil {
		t.Fatalf("RenderSPA failed: %v", err)
	}
	if !strings.Contains(html, "Rendered Content for https://example.com/spa") {
		t.Errorf("Unexpected rendered HTML output: %s", html)
	}

	// Playwright engine
	enginePW := NewFallbackRenderEngine("playwright")
	htmlPW, err := enginePW.RenderSPA(ctx, "https://example.com/spa2")
	if err != nil {
		t.Fatalf("RenderSPA failed for Playwright: %v", err)
	}
	if !strings.Contains(htmlPW, "Engine: playwright") {
		t.Errorf("Expected Playwright engine output, got: %s", htmlPW)
	}

	// Aborted resource URL
	_, err = engine.RenderSPA(ctx, "https://example.com/banner.png")
	if err == nil {
		t.Fatalf("Expected error rendering media URL, got nil")
	}

	// Custom render fn
	customEngine := NewFallbackRenderEngine("custom")
	customEngine.CustomRenderFn = func(ctx context.Context, targetURL string) (string, error) {
		return "<html><body><h1>Custom SPA</h1></body></html>", nil
	}
	customHTML, err := customEngine.RenderSPA(ctx, "https://example.com/custom")
	if err != nil {
		t.Fatalf("RenderSPA custom failed: %v", err)
	}
	if customHTML != "<html><body><h1>Custom SPA</h1></body></html>" {
		t.Errorf("Unexpected custom HTML: %s", customHTML)
	}

	// Unsupported engine
	badEngine := NewFallbackRenderEngine("unsupported_type")
	_, err = badEngine.RenderSPA(ctx, "https://example.com/test")
	if err == nil {
		t.Fatalf("Expected error for unsupported engine type, got nil")
	}
}

func TestRenderSPA_PlaywrightErrorResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error in playwright service"))
	}))
	defer ts.Close()

	t.Setenv("PLAYWRIGHT_SERVICE_URL", ts.URL)

	engine := NewFallbackRenderEngine("playwright")
	// Should cleanly close response and fallback to mock content
	html, err := engine.RenderSPA(context.Background(), "https://example.com/test-spa")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(html, "Rendered Content for https://example.com/test-spa") {
		t.Errorf("expected fallback mock HTML, got: %s", html)
	}
}

func TestRenderSPA_ChromeExplicitFailure(t *testing.T) {
	t.Setenv("ENABLE_HEADLESS_CHROME", "true")
	t.Setenv("CHROME_PATH", "/non/existent/chrome/binary/path/12345")

	engine := NewFallbackRenderEngine("chrome")
	_, err := engine.RenderSPA(context.Background(), "https://example.com/spa")
	if err == nil {
		t.Fatalf("Expected explicit error on headless chrome failure when enabled, got nil")
	}
	if !strings.Contains(err.Error(), "chrome execution failed") {
		t.Errorf("Expected 'chrome execution failed' in error message, got: %v", err)
	}
}
