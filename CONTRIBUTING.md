# Contributing to WebLimbAI

Thank you for your interest in contributing to **WebLimbAI**. We welcome contributions from open-source developers, systems engineers, AI builders, and researchers.

WebLimbAI is an embedded and distributed web crawler, hybrid BM25 + Vector search engine, and Model Context Protocol (MCP) server written in **Go**.

Follow this guide to set up your local development environment, understand the monorepo architecture, and submit pull requests.

---

## Table of Contents
- [Code of Conduct](#code-of-conduct)
- [Prerequisites](#prerequisites)
- [Local Development Setup](#local-development-setup)
- [Monorepo Architecture Overview](#monorepo-architecture-overview)
- [Coding Standards & Safety Guidelines](#coding-standards--safety-guidelines)
- [Testing & Verification](#testing--verification)
- [Submitting a Pull Request](#submitting-a-pull-request)

---

## Code of Conduct
We are committed to fostering an open, welcoming, and inclusive community. Please treat all contributors and maintainers with respect.

---

## Prerequisites

Before building or running WebLimbAI, ensure you have:
- **Go**: Version `1.22` or higher (`go version`)
- **Docker & Docker Compose**: Version `20.10+` (optional, for PowerLimbs distributed mode)
- **Git**: For source control management

---

## Local Development Setup

### 1. Clone the Repository
```bash
git clone https://github.com/anishraj836/WebLimbAI.git
cd WebLimbAI
```

### 2. Build the Embedded LightLimbs CLI
```bash
go mod download
go build -o lightlimbs ./cmd/lightlimbs
```

### 3. Run LightLimbs Locally
```bash
# Start embedded HTTP API and MCP server on port 8080
./lightlimbs serve --port 8080

# Or run stdio MCP mode directly
./lightlimbs serve --mcp
```

### 4. (Optional) Run PowerLimbs Distributed Stack
For multi-node distributed development with Kafka, PostgreSQL, and Redis:

```bash
docker-compose up -d
```

---

## Monorepo Architecture Overview

WebLimbAI is structured into clean modular packages:

```text
crawler-monorepo/
├── cmd/
│   ├── lightlimbs/          # Single-binary embedded server & universal CLI
│   └── seed_sde_corpus/     # Batch 1,000+ SDE corpus ingestion CLI tool
├── internal/
│   ├── cluster/             # PowerLimbs: Raft consensus, Hash Ring, Scatter-Gather
│   ├── crawler/             # Adaptive entropy priority frontier, anti-bot HTTP client
│   ├── extractor/           # HTML DOM AST parser -> Markdown, JSON Schema
│   ├── index/               # Inverted Index (VByte + Block-Max WAND), Vector Index (Int8)
│   ├── search/              # Hybrid RRF, Re-ranking, Metasearch, DeepSeek reasoning
│   ├── storage/             # PostgreSQL pool, schema migrations, TTL janitor, JSON fallback
│   └── mcp/                 # Model Context Protocol stdio handler & auto-configurator
├── common/                  # Shared middleware, ratelimit, config, webhook, kafka, logger
├── mcp-server/              # Model Context Protocol stdio binary entrypoint
├── agent-service/           # PowerLimbs: Agent microservice (:8090)
├── search-service/          # PowerLimbs: Search microservice (:8088)
├── indexer-service/         # PowerLimbs: Kafka indexer worker service
└── docker-compose.yml       # Distributed cluster service definitions
```

---

## Coding Standards & Safety Guidelines

1. **Race Safety**: All shared data structures (Inverted Index, Trie, Vector Index) must be protected with `sync.RWMutex` or atomic primitives. All PRs must pass `go test -race ./...`.
2. **Memory Bounding**: Always wrap external HTTP responses in `io.LimitReader(resp.Body, 10*1024*1024)` (10MB safety limit) to prevent memory exhaustion on untrusted web pages.
3. **HTTP Keep-Alive Socket Draining**: Drain unread HTTP response bodies with `io.Copy(io.Discard, io.LimitReader(resp.Body, 512*1024))` before closing.
4. **Error Handling**: Do not panic in library or request handler paths. Return explicit Go errors or appropriate HTTP status codes.

---

## Testing & Verification

Run the full monorepo race detector suite before opening a pull request:

```bash
# Run unit & race condition tests across all packages
go test -race -v ./...

# Run static analysis
go vet ./...
```

---

## Submitting a Pull Request

1. Fork the repository and create a feature branch (`git checkout -b feat/my-improvement`).
2. Make your code changes adhering to our coding standards.
3. Add corresponding unit tests in `*_test.go`.
4. Run `go test -race ./...` and ensure all 23 packages pass cleanly.
5. Commit your changes with conventional commit messages (`feat: ...`, `fix: ...`, `docs: ...`).
6. Push to your fork and submit a Pull Request to `main`.
