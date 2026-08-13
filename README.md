# 🦾 AgentLimbs

> **High-Performance Web Crawler, Hybrid RAG Search Engine, DuckDuckGo Metasearch, DeepSeek Agentic Reasoner, and Model Context Protocol (MCP) Server for AI Agents.**

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![MCP Compatible](https://img.shields.io/badge/MCP-Standard-purple.svg)](https://modelcontextprotocol.io/)

**AgentLimbs** is a production-grade, token-efficient web ingestion, HTML extraction, and hybrid RAG search platform built in **Go**. It empowers AI Agents, LLM pipelines, and developer workflows to scrape, parse, index, and query web pages with microsecond-to-millisecond search latencies.

---

## 🌟 Key Capabilities

- **🚀 Dual Deployment Modes**: Run as a lightweight, single-binary embedded server (`cmd/agentlimbs-light`) with zero external dependencies, or as a distributed 9-microservice event-driven stack via Docker Compose.
- **🌳 HTML DOM AST Extractor (`internal/extractor`)**: Replaces fragile regex stripping with official Go DOM tree walking (`golang.org/x/net/html`) to convert web pages into clean, token-efficient Markdown (up to **82% token savings** over raw HTML, averaging **5.6x reduction**).
- **🌐 Live DuckDuckGo Metasearch Engine (`internal/search/metasearch.go`)**: Performs real-time web engine queries, fetches target links concurrently, parses DOM into RAG Markdown, and indexes web results on-the-fly into memory.
- **🤖 DeepSeek Agentic AI Reasoning (`internal/search/agentic.go`)**: Autonomous multi-step RAG pipeline (Query Decomposition → Live Web & Vector Ingestion → DeepSeek V3/R1 Context Reasoning & Answer Synthesis with inline citations).
- **🔍 Hybrid RRF Search Engine & Re-ranking (`internal/search`)**: Blends sparse keyword matching (**Okapi BM25**) and dense semantic vector similarity (**Subword 3-gram/4-gram L2 normalized vectors**) using Reciprocal Rank Fusion (k=60), followed by post-RRF candidate re-ranking via `ComputeKeywordTitleBoostScore` (computes exact keyword and title frequency boosts).
- **⚡ Event-Driven Incremental Indexing (`search-service`)**: Replaces O(N) DB scan index wipes with single-document `IndexDocumentIncrementalByURL` triggered by Kafka `index_updates` events, updating BM25, Trie, and Vector stores incrementally without wiping in-memory state.
- **⏳ Document Time-To-Live (TTL) & Janitor (`internal/storage`)**: Instant query-time expiration filtering (0ms delay) combined with a background cleanup janitor. Fully environment-configurable via `DEFAULT_TTL_SECONDS` and `JANITOR_INTERVAL`.
- **🛡️ Anti-Bot Header Profiles & SPA Stepping (`internal/crawler`)**: Rotates modern Chrome 122 `Sec-Ch-Ua` header profiles, implements `FetchWithStepping` with jittered backoff on HTTP 403/401/429, and detects empty JavaScript SPA root containers.
- **🔌 Native Model Context Protocol (MCP)**: Directly integrates with Antigravity, Cursor, and Claude Desktop via stdio JSON-RPC tool calls (`agent_limbs_scrape`, `agent_limbs_hybrid_search`).

---

## 🏗️ Architecture

AgentLimbs is designed as a **Modular Monolith** with clear `internal/` package encapsulation:

```text
crawler-monorepo/
├── cmd/
│   ├── agentlimbs-light/    # Lightweight single-binary embedded server & CLI
│   └── seed_sde_corpus/     # Batch SDE corpus ingestion script
├── internal/
│   ├── crawler/             # Stepping HTTP client, Chrome 122 headers, robots.txt cache
│   ├── extractor/           # HTML DOM AST parser (golang.org/x/net/html) -> Markdown
│   ├── index/               # BM25 inverted index, trie autocomplete, subword vector store, embedders
│   ├── search/              # Hybrid Reciprocal Rank Fusion (RRF), DuckDuckGo metasearch, DeepSeek agentic pipeline
│   └── storage/             # PostgreSQL pool, schema migrations, TTL janitor, file fallback
├── common/                  # Shared middleware, ratelimit, config, webhook, kafka, logger
├── mcp-server/              # Model Context Protocol stdio server binary
└── docker-compose.yml       # Distributed 9-microservice deployment stack
```

---

## ⚡ Quick Start

### 1. Single-Binary Mode (Recommended for Local Dev & RAG)

Run AgentLimbs as a single embedded Go process in 2 seconds:

```bash
# Clone and run single-binary server
go run cmd/agentlimbs-light/main.go
```

The server boots instantly on port **`8080`** with file snapshot storage (`data/`).

---

### 2. Scrape & Auto-Index a Page

Scrape any website URL, convert it to clean Markdown, and automatically index it into the BM25 and Vector search engines:

```bash
curl -X POST http://localhost:8080/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://go.dev",
    "mode": "clean_rag",
    "ttl_seconds": 86400
  }'
```

**Response:**
```json
{
  "url": "https://go.dev",
  "title": "The Go Programming Language",
  "markdown": "# The Go Programming Language\n\nBuild simple, secure, scalable systems...",
  "token_estimate": 142,
  "latency_ms": 32.4
}
```

---

### 3. Query Hybrid RRF Search

Execute hybrid keyword + vector semantic search over your indexed corpus:

```bash
curl -X POST http://localhost:8080/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "goroutine GMP scheduler concurrency",
    "top_k": 5
  }'
```

---

### 4. Live DuckDuckGo Web Search

Query the live web using the DuckDuckGo Metasearch engine:

```bash
curl -X POST http://localhost:8080/v1/web-search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "go 1.23 release notes",
    "top_k": 5
  }'
```

---

### 5. DeepSeek Agentic AI Reasoning

Run an autonomous multi-step RAG reasoning loop using DeepSeek V3/R1:

```bash
curl -X POST http://localhost:8080/v1/agentic-search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What are the key features and concurrency performance gains in Go 1.23?",
    "model": "deepseek-chat",
    "llm_api_key": "sk-...",
    "top_k": 5
  }'
```

---

## ⚙️ Environment Variables

Configure AgentLimbs using environment variables in `.env` or system shell:

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PORT` | HTTP server port for `agentlimbs-light` | `8080` |
| `EMBEDDING_PROVIDER` | Pluggable embedding engine (`default`, `cohere`, `openai`, `ollama`) | `default` *(128-D Subword)* |
| `COHERE_API_KEY` | API Key when `EMBEDDING_PROVIDER=cohere` | *(Optional)* |
| `OPENAI_API_KEY` | API Key when `EMBEDDING_PROVIDER=openai` | *(Optional)* |
| `DEEPSEEK_API_KEY` | API Key for DeepSeek agentic search reasoning | *(Optional)* |
| `OLLAMA_HOST` | Host URL when `EMBEDDING_PROVIDER=ollama` | `http://localhost:11434` |
| `DEFAULT_TTL_SECONDS` | Default page expiration fallback (seconds) | `604800` *(7 days)* |
| `JANITOR_INTERVAL` | Background TTL janitor cleanup frequency | `15m` *(15 minutes)* |
| `AGENT_API_KEY` | Secret API Key for HTTP endpoints (`X-API-Key`) | *(Optional)* |
| `TRUSTED_PROXIES` | Comma-separated CIDRs/IPs trusted for rate limit headers | `127.0.0.1,::1` |
| `DATABASE_URL` | PostgreSQL connection string (falls back to file storage if unset) | `postgres://...` |

---

## 🔌 Model Context Protocol (MCP) Integration

Connect AgentLimbs to your AI IDE (Antigravity, Cursor, Claude Desktop) as a tool provider:

### `.agents/mcp_config.json`

```json
{
  "mcpServers": {
    "agent-limbs": {
      "command": "go",
      "args": ["run", "mcp-server/main.go"]
    }
  }
}
```

### Available MCP Tools

1. **`agent_limbs_scrape`**: Scrapes target URL, extracts clean Markdown, and auto-indexes it with optional `ttl_seconds`.
2. **`agent_limbs_hybrid_search`**: Searches the indexed corpus using hybrid BM25 + Vector RRF ranking.

---

## 🧪 Running Tests

Run the complete workspace test suite (all unit & integration tests):

```bash
go test -v ./...
```

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for details.
