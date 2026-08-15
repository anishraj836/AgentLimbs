# 🦾 WebLimbAI — Developer & AI Agent Guidelines

This repository contains **WebLimbAI**, a high-performance web crawler, indexing engine, vector embedding store, and Anthropic Model Context Protocol (MCP) server written in Go.

---

## 🛠️ Build & Test Commands

- **Run All Unit Tests**: `go test -v ./...`
- **Run Thread Race Detector**: `go test -race -v ./...`
- **Run Static Analysis**: `go vet ./...`
- **Build MCP Server Executable**: `go build -o mcp-server/agent-limbs-mcp mcp-server/main.go`
- **Run Agent REST API Service**: `go run agent-service/main.go`
- **Run Full Microservice Stack**: `docker-compose up -d`

---

## 🏛️ Core Architecture Principles

1. **Thread-Safety & Data Races**:
   - Always wrap shared map mutations (Inverted Index, Trie, Vector Index, Web Graph) in `sync.RWMutex` locks.
   - Enforce clean execution under Go's race detector (`go test -race`).

2. **Memory Safety & Limits**:
   - Always enforce `io.LimitReader(resp.Body, 10*1024*1024)` (10MB safety cap) when fetching web pages to prevent OOM memory explosions.
   - Restrict worker routines using bounded concurrency pools (`concurrencyLimit: 50`).

3. **Kafka Zero-Data-Loss Offset Tracking**:
   - Use `common/kafka/offset_tracker.go` for contiguous message offset tracking to prevent message loss during worker crashes.

4. **DOM Extraction Modes (`common/markdown/markdown.go`)**:
   - `clean_rag` (Default): Strips sidebars/TOC and same-page anchor URLs (`#GOGC`) for 85%+ token reduction.
   - `preserve_links`: Preserves all Markdown URLs (including fragment links).
   - `raw`: Minimal filtering; preserves original DOM structure.

5. **Anthropic Model Context Protocol (MCP)**:
   - MCP Server is implemented in `mcp-server/main.go` and `common/mcp/protocol.go` following MCP version `2024-11-05`.
   - Exposes tools: `agent_limbs_scrape` and `agent_limbs_hybrid_search`.
