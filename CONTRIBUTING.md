# Contributing to WebLimbAI 🦾

Thank you for your interest in contributing to **WebLimbAI**! We welcome contributions from open-source developers, systems engineers, AI builders, and researchers.

WebLimbAI is a high-throughput distributed web crawler, hybrid BM25 + Vector RRF search engine, and Anthropic Model Context Protocol (MCP) server written in **Go**.

Follow this guide to get your local development environment set up, understand our monorepo architecture, and submit high-quality pull requests.

---

## 📋 Table of Contents
- [Code of Conduct](#-code-of-conduct)
- [Prerequisites](#-prerequisites)
- [Local Development Setup](#-local-development-setup)
- [Monorepo Architecture Overview](#-monorepo-architecture-overview)
- [Coding Standards & Security Hygiene](#-coding-standards--security-hygiene)
- [Testing & Verification](#-testing--verification)
- [Submitting a Pull Request](#-submitting-a-pull-request)

---

## 📜 Code of Conduct
We are committed to fostering an open, welcoming, and inclusive community. Please treat all contributors and maintainers with respect.

---

## 🛠️ Prerequisites

Before building or running WebLimbAI, ensure you have the following installed:
- **Go**: Version `1.21` or higher (`go version`)
- **Docker & Docker Compose**: Version `20.10+` (`docker compose version`)
- **Git**: For source control management

---

## 🚀 Local Development Setup

### 1. Clone the Repository
```bash
git clone https://github.com/anishraj836/WebLimbAI.git
cd WebLimbAI
```

### 2. Start Infrastructure Services
WebLimbAI relies on Docker Compose to orchestrate background infrastructure (Apache Kafka, Zookeeper, Redis, PostgreSQL, Prometheus, and Grafana):

```bash
docker-compose up -d
```

Verify all containers are running:
```bash
docker-compose ps
```

### 3. Build the Monorepo
Download dependencies and build all microservices and the MCP server binary:

```bash
go mod download
go build -o mcp-server/agent-limbs-mcp ./mcp-server/main.go
```

### 4. Run Microservices Locally
You can run individual microservices locally for testing:

```bash
# Run Agent Service (AI Scrape & Hybrid RAG REST API)
EXECUTION_MODE=local go run ./agent-service/main.go

# Run MCP Server (Stdio Interface for Claude Desktop & Cursor)
go run ./mcp-server/main.go
```

---

## 📁 Monorepo Architecture Overview

WebLimbAI is organized into clean Go microservices and shared packages:

```text
crawler-monorepo/
├── agent-service/          # REST API (/v1/scrape, /v1/agent/query) & Auth Middleware
├── crawler-service/        # Worker pool, HTTP client with SSRF guards & storage
├── frontier-service/       # Crawl frontier seed manager & Redis Bloom Filter
├── indexer-service/        # BM25 Inverted Index & PostgreSQL persistence engine
├── parser-service/         # HTML DOM parser, link extractor, & clean markdown generator
├── mcp-server/             # Anthropic Model Context Protocol (MCP) stdio server
├── common/                 # Shared core packages
│   ├── bm25/               # BM25 ranking algorithm & snippet highlighter
│   ├── db/                 # PostgreSQL schema & pgxpool document persistence
│   ├── hybrid/             # Reciprocal Rank Fusion (RRF) algorithm
│   ├── markdown/           # HTML-to-Markdown conversion engine
│   ├── mcp/                # MCP protocol specification handlers (v2024-11-05)
│   ├── robotstxt/          # Robots.txt compliance parser
│   ├── trie/               # O(L) Trie autocomplete data structure
│   └── vector/             # Dense feature vector generator & cosine similarity
└── docker-compose.yml      # Infrastructure service definitions
```

---

## 🔒 Coding Standards & Security Hygiene

To maintain enterprise-grade security and code quality, adhere to these house rules when submitting code:

1. **Concurrency Safety**:
   - Always wrap shared map reads and mutations in `sync.RWMutex` locks or call thread-safe getters (e.g., `GetMetadataMaps()`).
   - Never mutate shared global state without mutex protection.

2. **Network & SSRF Guards**:
   - Never use standard `http.Get` directly for arbitrary user URLs. Always route network fetches through `crawler-service/httpclient`, which enforces private IP checks (`isPrivateIP`) and pinned DNS dialing (`DialContext`).

3. **Resource & Memory Safety**:
   - Wrap untrusted HTTP body stream reads in `io.LimitReader` (default 10MB limit) to prevent out-of-memory (OOM) panics from zip bombs or large files.

4. **Database Security**:
   - Use 100% prepared parameterized SQL queries (`$1, $2, $3`). Never use string concatenation (`fmt.Sprintf`) inside SQL statements.

---

## 🧪 Testing & Verification

Before opening a Pull Request, you **must** run the test suite and static analysis tools.

### 1. Run Unit Tests
```bash
go test -v ./...
```

### 2. Run Data Race Detector
Ensure 0 data races exist across all concurrent packages:
```bash
go test -race -v ./...
```

### 3. Run Static Analysis (`go vet`)
```bash
go vet ./...
```

All three commands must complete cleanly with **0 errors and 0 data races**.

---

## 📤 Submitting a Pull Request

1. **Fork the Repository** and create a feature branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Commit Your Changes** with clear, descriptive commit messages:
   ```bash
   git commit -m "Add feature: support custom user-agent headers in agent-service"
   ```

3. **Push to Your Fork**:
   ```bash
   git push origin feature/your-feature-name
   ```

4. **Open a Pull Request** against the `main` branch of `https://github.com/anishraj836/WebLimbAI`.

### PR Checklist
- [ ] Code builds cleanly (`go build ./...`).
- [ ] All unit tests pass (`go test -v ./...`).
- [ ] Race detector reports 0 data races (`go test -race ./...`).
- [ ] `go vet ./...` reports 0 warnings.
- [ ] Clear description of the changes and motivation included in PR.

---

Thank you for helping build **WebLimbAI**! 🦾🚀
