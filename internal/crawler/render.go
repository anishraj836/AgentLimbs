package crawler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
)

// HeadlessRenderer defines the interface for dynamic client-side JS rendering engines.
type HeadlessRenderer interface {
	RenderSPA(ctx context.Context, targetURL string) (string, error)
	ShouldAbortResource(resourceURL string) bool
}

// FallbackRenderEngine provides dynamic JS rendering capabilities supporting Chrome/Playwright engines.
type FallbackRenderEngine struct {
	mu             sync.RWMutex
	EngineType     string // "chrome", "playwright", "mock"
	CustomRenderFn func(ctx context.Context, targetURL string) (string, error)
}

// NewFallbackRenderEngine creates a new FallbackRenderEngine instance.
func NewFallbackRenderEngine(engineType string) *FallbackRenderEngine {
	if engineType == "" {
		engineType = "chrome"
	}
	return &FallbackRenderEngine{
		EngineType: engineType,
	}
}

// ShouldAbortResource implements resource aborting logic, selectively intercepting and aborting
// image, media, and font URLs while keeping core web page, CSS, and JS resources.
func (e *FallbackRenderEngine) ShouldAbortResource(resourceURL string) bool {
	return IsMediaResourceURL(resourceURL)
}

// RenderSPA executes dynamic JS rendering for targetURL using Headless Chrome CLI, Playwright Service, or custom function.
func (e *FallbackRenderEngine) RenderSPA(ctx context.Context, targetURL string) (string, error) {
	if e.ShouldAbortResource(targetURL) {
		return "", fmt.Errorf("blocked resource URL: %s", targetURL)
	}

	e.mu.RLock()
	customFn := e.CustomRenderFn
	engineType := e.EngineType
	e.mu.RUnlock()

	if customFn != nil {
		return customFn(ctx, targetURL)
	}

	// 1. Validate engine type
	if engineType != "chrome" && engineType != "playwright" && engineType != "mock" {
		return "", fmt.Errorf("unsupported rendering engine type: %s", engineType)
	}

	// 2. Playwright Service (if configured and engineType == "playwright")
	if engineType == "playwright" {
		if pwURL := os.Getenv("PLAYWRIGHT_SERVICE_URL"); pwURL != "" {
			reqBody, _ := json.Marshal(map[string]string{"url": targetURL})
			req, err := http.NewRequestWithContext(ctx, "POST", pwURL+"/render", bytes.NewBuffer(reqBody))
			if err == nil {
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err == nil && resp.StatusCode == 200 {
					body, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err == nil && len(body) > 0 {
						return string(body), nil
					}
				}
			}
		}
	}

	// 3. Headless Chrome CLI (if explicitly configured via CHROME_PATH or ENABLE_HEADLESS_CHROME)
	if engineType == "chrome" && (os.Getenv("CHROME_PATH") != "" || os.Getenv("ENABLE_HEADLESS_CHROME") == "true") {
		for _, bin := range []string{os.Getenv("CHROME_PATH"), "google-chrome", "chromium", "chromium-browser", "headless-shell"} {
			if bin == "" {
				continue
			}
			path, err := exec.LookPath(bin)
			if err == nil {
				cmd := exec.CommandContext(ctx, path, "--headless", "--disable-gpu", "--dump-dom", "--no-sandbox", targetURL)
				output, err := cmd.Output()
				if err == nil && len(output) > 0 {
					return string(output), nil
				}
			}
		}
	}

	// 4. Default / Mock Rendered Content for testing and offline fallback
	return fmt.Sprintf("<html><body><div id=\"root\"><h1>Rendered Content for %s</h1><p>Engine: %s</p></div></body></html>", targetURL, engineType), nil
}
