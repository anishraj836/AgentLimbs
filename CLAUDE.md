# WebLimbAI — Developer & AI Agent Guidelines

This repository contains **WebLimbAI**, an embedded and distributed web crawler, indexing engine, quantized vector store, and Anthropic Model Context Protocol (MCP) server written in Go.

---

## Build & Test Commands

- **Run All Unit Tests**: `go test -v ./...`
- **Run Thread Race Detector**: `go test -race -v ./...`
- **Run Static Analysis**: `go vet ./...`
- **Build LightLimbs CLI**: `go build -o lightlimbs ./cmd/lightlimbs`
- **Run Embedded Server**: `go run ./cmd/lightlimbs/main.go`
- **Run Full Distributed Stack**: `docker-compose up -d`

---

## Core Architecture Principles

1. **Thread Safety & Data Races**:
   - Always wrap shared map mutations (Inverted Index, Trie, Vector Index, Web Graph) in `sync.RWMutex` locks.
   - Enforce clean execution under Go's race detector (`go test -race`).

2. **Memory Safety & Limits**:
   - Always enforce `io.LimitReader(resp.Body, 10*1024*1024)` (10MB safety cap) when fetching web pages to prevent OOM memory explosions.
   - Restrict worker routines using bounded concurrency pools (`concurrencyLimit: 50`).

3. **HTTP Socket Draining**:
   - Always drain remaining response bytes via `io.Copy(io.Discard, io.LimitReader(resp.Body, 512*1024))` before closing `resp.Body` to ensure HTTP keep-alive socket reuse in `http.Transport`.

4. **DOM Extraction Modes (`internal/extractor/markdown.go`)**:
   - `clean_rag` (Default): Strips sidebars/TOC and same-page anchor URLs for 85%+ token reduction.
   - `preserve_links`: Preserves all Markdown URLs (including fragment links).
   - `raw`: Minimal filtering; preserves original DOM structure.

5. **Anthropic Model Context Protocol (MCP)**:
   - MCP Server is implemented in `internal/mcp/` and `cmd/lightlimbs/main.go` following MCP version `2024-11-05`.
   - Exposes tools: `agent_limbs_scrape` and `agent_limbs_hybrid_search`.
