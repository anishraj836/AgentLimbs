# WebLimbAI

> A self-hostable web retrieval and hybrid search engine for AI agents and developer workflows.

WebLimbAI crawls and extracts clean, token-efficient Markdown from web pages, indexes content incrementally using hybrid BM25 and dense vector retrieval, and exposes functionality over HTTP and the Model Context Protocol (MCP).

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![MCP Compatible](https://img.shields.io/badge/MCP-Standard-purple.svg)](https://modelcontextprotocol.io/)
[![OpenAPI 3.0](https://img.shields.io/badge/OpenAPI-3.0-green.svg)](openapi.json)

---

## Overview

Interacting with the web presents three common challenges for LLM agents:
1. **High Token Overhead**: Raw HTML containing scripts, styles, navigation bars, and cookie banners inflates context window usage.
2. **Third-Party API Dependency**: Relying on hosted scraping APIs introduces recurring latency, cost, vendor lock-in, and privacy concerns for internal data.
3. **Scraper and Search Decoupling**: Standard scrapers return raw text without persisting or indexing the retrieved content for downstream search.

WebLimbAI resolves these challenges by combining an anti-bot HTTP client, HTML DOM parser, in-memory inverted index (BM25 with Block-Max WAND), quantized vector index (Int8), and hybrid Reciprocal Rank Fusion (RRF) search into a single system.

```text
                     AI Agent (Claude / Cursor / Custom)
                                     │
                                    MCP
                                     │
                               ┌───────────┐
                               │ WebLimbAI │
                               └─────┬─────┘
                                     │
                    ┌────────────────┴────────────────┐
                    ↓                                 ↓
               Web Crawler                       Search Engine
                    │                                 │
             HTML -> Markdown                   BM25 + Vector RRF
                    │                                 │
                    └────────────────┬────────────────┘
                                     ↓
                             In-Memory Index
```

---

## Quick Start: LightLimbs CLI

Install WebLimbAI using the installer script or compile directly from source:

```bash
# 1-Line Installer (macOS & Linux, automatically detects architecture)
curl -sSfL https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.sh | sh

# Or build LightLimbs from source:
go build -o lightlimbs ./cmd/lightlimbs
```

### 1-Click MCP Setup (Cursor & Claude Desktop)

Configure the Model Context Protocol in Cursor or Claude Desktop:

```bash
# Configures Claude Desktop and Cursor IDE:
lightlimbs init-mcp

# Or preview the configuration JSON:
lightlimbs init-mcp --stdout
```

### CLI Commands

```bash
# Scrape a web page into clean, token-reduced Markdown:
lightlimbs scrape https://go.dev/doc/tutorial/getting-started

# Scrape and output structured JSON:
lightlimbs scrape https://go.dev -j | jq .

# Recursive adaptive entropy crawl:
lightlimbs crawl https://docs.docker.com -a -d 2

# Hybrid search (BM25 + Dense Vectors + RRF) over indexed pages:
lightlimbs search "goroutine channel concurrency" --top 5

# Seed 1,000+ SDE engineering documents into local memory:
lightlimbs seed

# Run the HTTP API and MCP daemon:
lightlimbs serve --port 8080
```

---

## Deployment Modes: LightLimbs vs PowerLimbs

WebLimbAI supports two deployment modes depending on scale and infrastructure requirements:

```text
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                   WebLimbAI                                     │
├───────────────────────────────────────┬─────────────────────────────────────────┤
│             LightLimbs Mode           │             PowerLimbs Mode             │
│        (Single-Binary Embedded)       │        (Distributed Raft & Kafka)       │
├───────────────────────────────────────┼─────────────────────────────────────────┤
│ • Zero external dependencies          │ • Multi-node horizontal scaling         │
│ • In-memory hybrid search (BM25+V)    │ • Partitioned Raft consensus sharding   │
│ • Local JSON snapshot file fallback   │ • Virtual node consistent hashing ring  │
│ • Stdio MCP server for AI IDEs        │ • Kafka stream-processing ingestion     │
│ • Memory footprint: ~25 MB            │ • Distributed scatter-gather coordinator│
│ • Ideal for: Local LLMs, AI IDEs, dev │ • Ideal for: Large-scale cloud clusters │
└───────────────────────────────────────┴─────────────────────────────────────────┘
```

```text
crawler-monorepo/
├── cmd/
│   ├── lightlimbs/          # LightLimbs single-binary embedded server & CLI
│   └── seed_sde_corpus/     # Batch 1,000+ SDE corpus ingestion CLI tool
├── internal/
│   ├── cluster/             # PowerLimbs: Raft consensus, Hash Ring, Scatter-Gather
│   ├── crawler/             # Adaptive entropy priority frontier, anti-bot client
│   ├── extractor/           # HTML DOM AST parser -> Markdown, JSON Schema
│   ├── index/               # Inverted Index (VByte + Block-Max WAND), Vector Index (Int8)
│   ├── search/              # Hybrid RRF, Re-ranking, Metasearch, DeepSeek reasoning
│   ├── storage/             # PostgreSQL pool, schema migrations, TTL janitor, JSON fallback
│   └── mcp/                 # Model Context Protocol stdio handler & auto-configurator
├── common/                  # Shared middleware, ratelimit, config, webhook, kafka, logger
├── mcp-server/              # Model Context Protocol stdio entrypoint
├── agent-service/           # PowerLimbs: Agent microservice (:8090)
├── search-service/          # PowerLimbs: Search microservice (:8088)
├── indexer-service/         # PowerLimbs: Kafka indexer worker service
└── docker-compose.yml       # PowerLimbs distributed deployment stack
```

### Module Delineation

- **`internal/index` (Core Indexing Data Structures)**:
  - **`InvertedIndex`**: In-memory Okapi BM25 inverted index supporting document frequency, term positions, Porter stemming, and BM25 weighting.
  - **`TrieNode` / `Trie`**: Prefix tree powering autocomplete suggestions (`/v1/autocomplete`).
  - **`VectorStore`**: Quantized 128-dimensional dense vector store (Int8, Float32, Float64) with SIMD-aligned dot product and cosine similarity ranking.
  - **`Engine`**: Thread-safe coordinator orchestrating inverted, vector, and trie structures across 64 partitioned metadata mutex shards.

- **`internal/search` (Search & Retrieval Orchestration)**:
  - **`ReciprocalRankFusion`**: Combines BM25 keyword rankings and dense vector rankings into a fused score using \(RRF(d) = \sum \frac{1}{60 + rank}\).
  - **Re-ranking**: Post-RRF keyword title boosting for exact phrase and term matches.
  - **`MetasearchAdapter`**: Metasearch orchestrator supporting singleflight deduplication and concurrent URL fetching.
  - **`AgenticPipeline`**: Multi-step query decomposition, retrieval, and LLM reasoning with deterministic local fallback synthesis when external LLMs are not configured.

---

## Running the Servers

### 1. LightLimbs Embedded Mode (Single Binary)

```bash
go run cmd/lightlimbs/main.go
```

The server starts on port **`8080`** with local JSON snapshot persistence in `data/`.

### 2. PowerLimbs Distributed Mode (Docker Compose)

```bash
docker-compose up -d
```

Starts the distributed stack with PostgreSQL, Kafka, Redis, `agent-service` (:8090), `search-service` (:8088), and `indexer-service`.

---

## REST API Reference

All endpoints support CORS and return JSON responses.

### Endpoint Summary

| Endpoint | Method | Mode | Description |
| :--- | :--- | :--- | :--- |
| [`/health`](#1-get-health) | `GET` | Both | System health status & execution mode |
| [`/v1/scrape`](#2-postget-v1scrape) | `POST`/`GET` | Both | Scrape web page into clean Markdown & auto-index |
| [`/v1/search`](#3-postget-v1search) | `POST`/`GET` | Both | Hybrid BM25 + Vector RRF search over indexed corpus |
| [`/v1/web-search`](#4-postget-v1web-search) | `POST`/`GET` | Both | Live metasearch with instant RAG ingestion |
| [`/v1/agentic-search`](#5-postget-v1agentic-search) | `POST`/`GET` | Both | Multi-step reasoning and answer synthesis |
| [`/v1/autocomplete`](#6-get-v1autocomplete) | `GET` | Both | Prefix autocomplete query suggestions |
| [`/v1/extract`](#7-post-v1extract) | `POST` | Agent Service | Extract structured JSON fields from web URL |
| [`/v1/agent/query`](#8-postget-v1agentquery) | `POST`/`GET` | Agent Service | Microservice hybrid RRF search over corpus |
| [`/v1/agent/tools`](#9-get-v1agenttools) | `GET` | Agent Service | Tool schemas for AI agent function calling |

---

### Authentication & Request Tracking

When `AGENT_API_KEY` is configured, requests must supply the API key using one of:
- Header: `X-API-Key: <key>`
- Header: `Authorization: Bearer <key>`
- Query Parameter: `?api_key=<key>`

All API responses include an `X-Request-ID` header. If passed in the request, it is echoed back; otherwise, a unique UUID v4 is generated.

---

### Endpoint Specifications

#### 1. `GET /health`

Checks server health and active deployment mode.

```bash
curl -X GET http://localhost:8080/health
```

Response:
```json
{"status":"ok","mode":"single_binary_embedded"}
```

---

#### 2. `POST/GET /v1/scrape`

Scrapes a web page URL, converts HTML to clean Markdown, and indexes the document into the BM25 and Vector search engines.

- **Parameters**:
  - `url` (string, required): Target URL.
  - `mode` (string, optional, default: `"clean_rag"`): Extraction mode (`"clean_rag"`, `"preserve_links"`, `"raw"`).
  - `ttl_seconds` (integer, optional, default: `604800`): Time-to-live expiration in seconds (clamped to 300s–2592000s).

```bash
curl -X POST http://localhost:8080/v1/scrape \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://go.dev",
    "mode": "clean_rag",
    "ttl_seconds": 86400
  }'
```

Response:
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

#### 3. `POST/GET /v1/search`

Queries the indexed corpus using Hybrid Reciprocal Rank Fusion.

- **Parameters**:
  - `query` / `q` (string, required): Natural language search query.
  - `top_k` (integer, optional, default: `5`, bounds: `1`–`100`): Maximum results count.
  - `mode` (string, optional, default: `"hybrid"`): Search algorithm (`"hybrid"`, `"bm25"`, `"vector"`).

```bash
curl -X POST http://localhost:8080/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "goroutine GMP scheduler concurrency",
    "top_k": 5
  }'
```

---

#### 4. `POST/GET /v1/web-search`

Executes live metasearch, fetches top results concurrently, converts HTML to Markdown, and indexes the content on the fly.

- **Parameters**:
  - `query` / `q` (string, required): Web search query.
  - `top_k` (integer, optional, default: `5`, bounds: `1`–`50`): Maximum results count.

```bash
curl -X POST http://localhost:8080/v1/web-search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "go 1.23 release notes",
    "top_k": 5
  }'
```

---

#### 5. `POST/GET /v1/agentic-search`

Executes multi-step reasoning: query decomposition, live web & vector retrieval, and LLM context synthesis with inline citations.

- **Parameters**:
  - `query` / `q` (string, required): Reasoning prompt.
  - `model` (string, optional, default: `"deepseek-chat"`): LLM model identifier.
  - `llm_api_key` (string, optional): Provider API key override.
  - `llm_base_url` (string, optional): Provider base URL override.
  - `top_k` (integer, optional, default: `5`): Retrieval depth.

```bash
curl -X POST http://localhost:8080/v1/agentic-search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What are the key features and performance gains in Go 1.23?",
    "model": "deepseek-chat",
    "top_k": 5
  }'
```

---

#### 6. `GET /v1/autocomplete`

Fast in-memory Trie prefix search for query suggestions.

- **Parameters**:
  - `q` (string, required): Prefix string.
  - `limit` (integer, optional, default: `5`, bounds: `1`–`50`): Maximum suggestions.

```bash
curl -X GET "http://localhost:8080/v1/autocomplete?q=goro&limit=5"
```

---

#### 7. `POST /v1/extract`

Fetches a web URL, parses HTML into Markdown, and extracts specific structured schema fields into JSON.

```bash
curl -X POST http://localhost:8090/v1/extract \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://go.dev",
    "fields": ["title", "overview", "concurrency_features"]
  }'
```

---

#### 8. `POST/GET /v1/agent/query`

Microservice API endpoint for executing hybrid RRF search across the distributed index.

```bash
curl -X POST http://localhost:8090/v1/agent/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Raft consensus leader election",
    "top_k": 5
  }'
```

---

#### 9. `GET /v1/agent/tools`

Returns OpenAI-compatible tool definitions for integration into agent tool-calling loops.

```bash
curl -X GET http://localhost:8090/v1/agent/tools
```

---

## Security & Rate Limiting

- **LightLimbs Embedded Server (`cmd/lightlimbs`)**:
  - Token bucket rate limiter configured at 50 requests/sec with a burst capacity of 100 requests.
- **Agent Service (`agent-service`)**:
  - Dynamic sliding-window rate limiter evaluated per client IP and `/24` IPv4 (or `/64` IPv6) subnet:
    - **Local Mode (`EXECUTION_MODE=local`)**: 1,800 req/min (30 req/sec).
    - **Cloud Mode (`EXECUTION_MODE=cloud`)**: 300 req/min (5 req/sec).
- **Client IP Resolution**:
  - Validates `RemoteAddr` against `TRUSTED_PROXIES` (default: `127.0.0.1,::1`). Trusted proxies parse `X-Forwarded-For` right-to-left to prevent IP spoofing.

---

## Model Context Protocol (MCP) Integration

Connect WebLimbAI to your AI editor (Cursor, Claude Desktop) as a stdio tool provider.

### Stdio Configuration (`mcp_config.json`)

```json
{
  "mcpServers": {
    "weblimb": {
      "command": "lightlimbs",
      "args": ["serve", "--mcp"]
    }
  }
}
```

### Available MCP Tools

- **`agent_limbs_scrape`**: Scrapes and extracts clean Markdown content from a web URL into the WebLimbAI search index.
- **`agent_limbs_hybrid_search`**: Performs hybrid BM25 and vector RAG search over indexed documents.

---

## Environment Variables

| Variable | Description | Default | Allowed Values |
| :--- | :--- | :--- | :--- |
| `PORT` | HTTP server port for `lightlimbs` | `8080` | Valid TCP port (e.g. `8080`) |
| `AGENT_PORT` | HTTP server port for `agent-service` | `8090` | Valid TCP port (e.g. `8090`) |
| `DATA_DIR` | Local snapshot directory path for embedded server | `data` | Valid filesystem path |
| `EXECUTION_MODE` | Deployment execution profile | `local` | `local`, `distributed`, `cloud`, `embedded` |
| `EMBEDDING_PROVIDER` | Vector embedding engine | `default` | `default` (128-D Subword), `cohere`, `openai`, `ollama` |
| `COHERE_API_KEY` | API Key when `EMBEDDING_PROVIDER=cohere` | *(Optional)* | Valid Cohere API key string |
| `OPENAI_API_KEY` | API Key when `EMBEDDING_PROVIDER=openai` | *(Optional)* | Valid OpenAI API key string |
| `OPENAI_BASE_URL` | Base URL endpoint for OpenAI provider | `https://api.openai.com/v1` | Valid URL string |
| `DEEPSEEK_API_KEY` | API Key for DeepSeek agentic search reasoning | *(Optional)* | Valid DeepSeek API key string |
| `DEEPSEEK_BASE_URL` | Base URL endpoint for DeepSeek provider | `https://api.deepseek.com/v1` | Valid URL string |
| `DEEPSEEK_MODEL` | LLM model identifier for agentic search | `deepseek-chat` | Model ID (e.g. `deepseek-chat`) |
| `OLLAMA_HOST` | Host URL when `EMBEDDING_PROVIDER=ollama` | `http://localhost:11434` | Valid HTTP URL |
| `DEFAULT_TTL_SECONDS` | Default document expiration (seconds) | `604800` | Positive integer (default 7 days) |
| `JANITOR_INTERVAL` | Background TTL cleanup frequency | `15m` | Valid duration string (e.g. `15m`, `1h`) |
| `AGENT_API_KEY` | Secret API Key for HTTP authentication | *(Optional)* | Secret string |
| `TRUSTED_PROXIES` | CIDRs/IPs trusted for rate limit headers | `127.0.0.1,::1` | Comma-separated IPs / CIDRs |
| `DATABASE_URL` | PostgreSQL connection string | *(Optional)* | `postgres://user:pass@host:port/dbname` |
| `KAFKA_BROKERS` | Kafka broker addresses for event indexing | *(Optional)* | `host1:9092,host2:9092` |

---

## SDE Corpus Seed Tool

WebLimbAI includes a batch corpus seed CLI tool to populate the vector, trie, and BM25 index engines with **1,000+ Software Development Engineering (SDE) technical documents** across 10 engineering domains (Data Structures, System Design, Storage Engines, OS Kernels, Computer Networking, Go Concurrency, Cloud/K8s, Design Patterns, Cryptography, ML Infra).

```bash
go run cmd/seed_sde_corpus/main.go
```

---

## High-Scale Performance Benchmarks

### 500,000-Document Benchmark

| Metric | Result | Notes |
| :--- | :--- | :--- |
| **Ingestion Throughput** | **93,542 docs/second** | Ingests 500k pages in **5.34 seconds** |
| **Active Heap Memory** | **1.27 GB RAM** | **2.61 KB per document** (postings, trie, 64 shards, vectors) |
| **p50 Search Latency** | **2.44 ms** | Sub-3ms hybrid BM25 + Vector + RRF ranking |
| **p90 Search Latency** | **3.38 ms** | Fast, responsive under load |
| **p95 Search Latency** | **4.00 ms** | Consistent percentile bounds |
| **p99 Search Latency** | **12.80 ms** | Guaranteed sub-15ms tail latency |
| **Kafka Offset Watermarking** | **1,302,192 msgs/sec** | 500k messages tracked across 8 partitions with 0 data loss |

### 1,000,000-Document Ingestion & Multi-Core Stress Benchmark

| Metric | Result | Notes |
| :--- | :--- | :--- |
| **Ingestion Throughput** | **22,279.2 docs/sec** | 1,000,000 docs indexed in **44.88s** across 4 concurrent workers |
| **Total Corpus Tokens** | **44,142,674 tokens** | Full Zipfian distribution (V=50,000 vocabulary) |
| **Post-GC Live Heap** | **3,047.53 MB** | **3.05 KB / document** (Inverted postings + Int8 vectors + Trie) |
| **Total GC Pause Time** | **25.14 ms** | Across 72 GC cycles during 1M ingestion (0.35ms avg pause) |
| **BM25 WAND Search Latency** | **P50: 23.32 ms \| P99: 57.06 ms** | Single-term and multi-term Block-Max dynamic pruning |
| **Two-Stage Hybrid Search Latency** | **P50: 48.42 ms \| P99: 93.30 ms** | BM25 candidate retrieval + Int8 vector rescoring + RRF merge |

---

## CPU, Concurrency & Resource Limits

### Default CPU Behavior
In all production binaries (`lightlimbs`, `agent-service`, `search-service`, `indexer-service`, `mcp-server`), Go automatically utilizes all available CPU cores on the host machine without artificial caps.

### Configuring Limits

#### 1. Via Environment Variable (`GOMAXPROCS`)
```bash
# Low-impact mode for local laptops:
GOMAXPROCS=2 ./lightlimbs

# High-throughput mode on production servers:
GOMAXPROCS=16 ./search-service
```

#### 2. Via Docker Container Limits
```bash
docker run -d \
  --cpus="4.0" \
  --memory="2g" \
  -p 8080:8080 \
  lightlimbs:latest
```

#### 3. Via Kubernetes Pod Limits
```yaml
resources:
  requests:
    cpu: "2"
    memory: "1Gi"
  limits:
    cpu: "8"
    memory: "4Gi"
```

---

## Running Tests

Run the full workspace unit and race test suite across all 23 packages:

```bash
go test -race -v ./...
```

---

## License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
