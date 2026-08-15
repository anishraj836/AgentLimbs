# 🦾 WebLimbAI

> **A self-hostable web retrieval layer for AI Agents.**  
> Give agents access to the web without coupling them to a third-party scraping or search API. WebLimbAI crawls and cleans web pages, incrementally indexes them using hybrid BM25 + semantic retrieval, and exposes the resulting capabilities through HTTP and MCP.

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![MCP Compatible](https://img.shields.io/badge/MCP-Standard-purple.svg)](https://modelcontextprotocol.io/)
[![OpenAPI 3.0](https://img.shields.io/badge/OpenAPI-3.0-green.svg)](openapi.json)

---

## 💡 The Problem & The Abstraction

Most AI agents face a core dilemma when interacting with the web:
1. **Raw Web Pages Waste Tokens**: Scraping raw HTML with `<script>`, `<style>`, and cookie banners burns thousands of context window tokens per request.
2. **Third-Party API Coupling**: Relying on SaaS APIs (like Tavily or Jina) introduces external API costs, latency, vendor lock-in, and privacy risks for internal data.
3. **Scrapers Aren't Search Engines**: Standard web scrapers only return raw Markdown—they don't store or search what they've scraped.

**WebLimbAI** acts as the dedicated **sensory and motor layer** for AI Agents. It receives raw web requests or natural language queries, cleans HTML into token-efficient Markdown, indexes content incrementally in-memory, and provides hybrid keyword+vector search across the agent's web memory.

```text
                     AI Agent (Claude / Ollama / Custom)
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
             HTML ➔ Markdown                    BM25 + Vector RRF
                    │                                 │
                    └────────────────┬────────────────┘
                                     ↓
                             In-Memory Index
```

---

## ⚡ Quick Start & Universal CLI (LightLimbs)

Install WebLimbAI via the universal 1-line script or build from source:

```bash
# 1-Line Universal Installer (macOS & Linux, detects arm64 / amd64 automatically)
curl -sSfL https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.sh | sh

# Or build LightLimbs from source:
go build -o lightlimbs ./cmd/lightlimbs
```

### 1-Click AI IDE MCP Setup (Cursor & Claude Desktop)
Auto-configure your AI editor's Model Context Protocol in **1 second** without editing JSON files:

```bash
# Auto-configures both Claude Desktop & Cursor IDE:
lightlimbs init-mcp

# Or preview the configuration snippet:
lightlimbs init-mcp --stdout
```

### Terminal Direct Scraping & Search
Use LightLimbs directly from your terminal or pipe into standard Unix tools:

```bash
# Scrape any web page into clean, token-reduced Markdown:
lightlimbs scrape https://go.dev/doc/tutorial/getting-started

# Scrape and output structured JSON with token savings (perfect for jq):
lightlimbs scrape https://go.dev -j | jq .

# Recursive adaptive entropy documentation crawl:
lightlimbs crawl https://docs.docker.com -a -d 2

# Hybrid search (BM25 + Dense Vectors + RRF) across locally indexed pages:
lightlimbs search "goroutine channel concurrency" --top 5

# Seed 1,000+ SDE engineering topics into local memory:
lightlimbs seed

# Run the background HTTP & MCP API daemon:
lightlimbs serve --port 8080
```

---

## 🏗️ Dual-Architecture: LightLimbs vs PowerLimbs

WebLimbAI provides two deployment topologies tailored for different scale requirements:

```text
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                   WebLimbAI                                     │
├───────────────────────────────────────┬─────────────────────────────────────────┤
│          ⚡ LightLimbs Mode           │           🚀 PowerLimbs Mode            │
│       (Single-Binary Embedded)        │      (Distributed Raft & Kafka)         │
├───────────────────────────────────────┼─────────────────────────────────────────┤
│ • Zero external dependencies          │ • Multi-node linear scale               │
│ • In-memory hybrid search (BM25+V)    │ • Partitioned Raft consensus sharding   │
│ • Local JSON snapshot persistence     │ • Virtual node consistent hashing ring  │
│ • 1-Click stdio MCP server for IDEs   │ • Kafka stream-processing pipeline      │
│ • Memory footprint: ~25 MB            │ • High-availability scatter-gather      │
│ • Target: Devs, Local LLMs, AI IDEs   │ • Target: High-throughput clusters      │
└───────────────────────────────────────┴─────────────────────────────────────────┘
```

```text
crawler-monorepo/
├── cmd/
│   ├── lightlimbs/          # LightLimbs single-binary embedded server & universal CLI
│   └── seed_sde_corpus/     # Batch 1,000+ SDE corpus ingestion CLI script
├── internal/
│   ├── cluster/             # PowerLimbs: Raft consensus, Consistent Hash Ring, Scatter-Gather
│   ├── crawler/             # Adaptive entropy priority frontier, anti-bot HTTP client
│   ├── extractor/           # HTML DOM AST parser (golang.org/x/net/html) -> Markdown, JSON Schema
│   ├── index/               # Inverted Index (VByte + Block-Max WAND), Quantized Vector Store (Int8)
│   ├── search/              # Hybrid RRF, Candidate Re-ranking, DuckDuckGo Metasearch, DeepSeek
│   ├── storage/             # PostgreSQL pool, schema migrations, TTL janitor, JSON snapshot fallback
│   └── mcp/                 # Model Context Protocol stdio JSON-RPC handler & 1-click configurator
├── common/                  # Shared middleware, ratelimit, config, webhook, kafka, logger, stemmer
├── mcp-server/              # Model Context Protocol stdio binary entrypoint
├── agent-service/           # PowerLimbs: Agent microservice (Port 8090)
├── search-service/          # PowerLimbs: Search microservice (Port 8088)
├── indexer-service/         # PowerLimbs: Kafka indexer worker service
└── docker-compose.yml       # PowerLimbs distributed deployment stack
```

### Module Delineation: Index vs Search

- **`internal/index` (Core Indexing Data Structures)**:
  - **`InvertedIndex`**: In-memory Okapi BM25 inverted index supporting document frequency, term position indexing, stemming, and BM25 term weighting.
  - **`TrieNode` / `Trie`**: In-memory prefix tree powering microsecond autocomplete (`/v1/autocomplete`).
  - **`VectorStore`**: L2-normalized 128-dimensional subword (3-gram/4-gram) dense vector store with cosine similarity ranking.
  - **`Engine`**: Unified thread-safe coordinator orchestrating inverted, vector, and trie structures.

- **`internal/search` (Search & Agentic Orchestration)**:
  - **`ReciprocalRankFusion`**: Merges BM25 keyword rankings and vector semantic rankings into a single fused score using standard RRF formula (\(RRF = \sum \frac{1}{60 + rank}\)).
  - **Re-ranking**: Applies `ComputeKeywordTitleBoostScore` post-RRF to boost exact title and term matches.
  - **`MetasearchAdapter`**: Live DuckDuckGo metasearch orchestrator:
    - **Singleflight Deduplication (`singleflight.Group`)**: Coalesces concurrent identical search queries to prevent duplicate outbound web requests.
    - **Errgroup Concurrency (`golang.org/x/sync/errgroup`)**: Fetches and parses up to 10 web search target URLs concurrently with context deadlines (1.5s timeout).
  - **`AgenticPipeline`**: DeepSeek multi-step reasoning orchestrator:
    - Performs query intent decomposition, live web & corpus retrieval, and DeepSeek LLM context reasoning.
    - **Local Deterministic Fallback (`fallbackLocalSynthesis`)**: If LLM API keys are omitted or external calls fail, automatically synthesizes a structured Markdown RAG summary with citations (`[1]`, `[2]`), source lineage, and scores.

---

## ⚡ Quick Start

### 1. Single-Binary Embedded Mode (Recommended)

Run WebLimbAI as a single embedded Go process in 2 seconds with zero external dependencies:

```bash
# Run single-binary server (boots on port 8080)
go run cmd/agentlimbs-light/main.go
```

The server boots instantly on port **`8080`** with local JSON snapshot storage (`data/`).

---

### 2. Distributed Microservices Mode

Run the complete distributed 9-microservice stack with PostgreSQL, Kafka, and Redis via Docker Compose:

```bash
docker-compose up -d
```

---

## 📡 Comprehensive REST API Reference

WebLimbAI exposes 9 HTTP API endpoints. All endpoints support CORS and return JSON responses.

### Summary of All 9 Endpoints

| Endpoint | Method | Mode | Description |
| :--- | :--- | :--- | :--- |
| [`/health`](#1-get-health) | `GET` | Both | System health status & execution mode |
| [`/v1/scrape`](#2-postget-v1scrape) | `POST`/`GET` | Both | Scrape web page into clean Markdown & auto-index |
| [`/v1/search`](#3-postget-v1search) | `POST`/`GET` | Light / Both | Hybrid BM25 + Vector RRF search over indexed corpus |
| [`/v1/web-search`](#4-postget-v1web-search) | `POST`/`GET` | Both | Live DuckDuckGo metasearch with instant RAG ingestion |
| [`/v1/agentic-search`](#5-postget-v1agentic-search) | `POST`/`GET` | Both | Autonomous DeepSeek multi-step RAG reasoning |
| [`/v1/autocomplete`](#6-get-v1autocomplete) | `GET` | Both | Microsecond prefix autocomplete suggestions |
| [`/v1/extract`](#7-post-v1extract) | `POST` | Agent Service | Extract structured JSON fields from web URL |
| [`/v1/agent/query`](#8-postget-v1agentquery) | `POST`/`GET` | Agent Service | Microservice hybrid RRF search over corpus |
| [`/v1/agent/tools`](#9-get-v1agenttools) | `GET` | Agent Service | OpenAI-format tool definitions for AI agents |

---

### Authentication Headers & Response Tracking

When `AGENT_API_KEY` is configured (or in `cloud` execution mode), requests must supply the API key using one of the following:

- **Header 1**: `X-API-Key: <key>`
- **Header 2**: `Authorization: Bearer <key>`
- **Query Parameter**: `?api_key=<key>`

All API responses include an **`X-Request-ID`** header. If passed in the request header `X-Request-ID`, it will be echoed; otherwise, a unique UUID v4 is generated.

---

### Endpoint Specifications & cURL Examples

#### 1. `GET /health`

Checks server health and active deployment mode.

- **Query Parameters**: None
- **Response**: `{"status":"ok","mode":"single_binary_embedded"}`

```bash
curl -X GET http://localhost:8080/health
```

---

#### 2. `POST/GET /v1/scrape`

Scrapes any HTTP/HTTPS website URL, converts the DOM into clean Markdown, and auto-indexes the page into the BM25 and Vector search engines.

- **Parameters & Bounds**:
  - `url` (string, **required**): Target web page URL.
  - `mode` (string, *optional*, default: `"clean_rag"`): Extraction mode. Allowed values: `"clean_rag"`, `"preserve_links"`, `"raw"`.
  - `ttl_seconds` (integer, *optional*, bounds: `300` to `2592000` [5 mins to 30 days]): Time-to-live expiration in seconds. Clamped to 300s minimum and 30 days maximum. Omitted or zero defaults to `DEFAULT_TTL_SECONDS` (7 days).

**cURL (POST):**
```bash
curl -X POST http://localhost:8080/v1/scrape \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your_secret_key" \
  -d '{
    "url": "https://go.dev",
    "mode": "clean_rag",
    "ttl_seconds": 86400
  }'
```

**cURL (GET):**
```bash
curl -X GET "http://localhost:8080/v1/scrape?url=https://go.dev&mode=clean_rag&ttl_seconds=86400"
```

**Response Body:**
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

Queries the indexed web corpus using Hybrid Reciprocal Rank Fusion (BM25 keyword matching + Subword dense vector semantic search).

- **Parameters & Bounds**:
  - `query` / `q` (string, **required**): Natural language search query.
  - `top_k` (integer, *optional*, default: `5`, bounds: `1` to `100`): Maximum results count.
  - `mode` (string, *optional*, default: `"hybrid"`): Search algorithm mode (`"hybrid"`, `"bm25"`, `"vector"`).

**cURL (POST):**
```bash
curl -X POST http://localhost:8080/v1/search \
  -H "Content-Type: application/json" \
  -d '{
    "query": "goroutine GMP scheduler concurrency",
    "top_k": 5
  }'
```

**cURL (GET):**
```bash
curl -X GET "http://localhost:8080/v1/search?q=goroutine+concurrency&top_k=5"
```

---

#### 4. `POST/GET /v1/web-search`

Performs live DuckDuckGo metasearch, fetches top web page links concurrently (`errgroup`), parses HTML DOM to Markdown, and indexes results on-the-fly.

- **Parameters & Bounds**:
  - `query` / `q` (string, **required**): Web search topic.
  - `top_k` (integer, *optional*, default: `5`, bounds: `1` to `50`): Maximum web hits.

**cURL (POST):**
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

Runs an autonomous multi-step RAG reasoning pipeline (Query Decomposition → Live Web/Vector Ingestion → DeepSeek Context Reasoning & Answer Synthesis with citations).

- **Parameters & Bounds**:
  - `query` / `q` (string, **required**): Reasoning question or prompt.
  - `model` (string, *optional*, default: `"deepseek-chat"`): LLM model identifier.
  - `llm_api_key` (string, *optional*): API key override for LLM provider.
  - `llm_base_url` (string, *optional*): LLM base URL endpoint override (e.g. `https://api.deepseek.com/v1`).
  - `top_k` (integer, *optional*, default: `5`): Retrieval depth per sub-query.

**cURL (POST):**
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

Fast in-memory Trie prefix search for live query suggestions.

- **Parameters & Bounds**:
  - `q` (string, **required**): Prefix string.
  - `limit` (integer, *optional*, default: `5`, bounds: `1` to `50`): Max suggestions.

**cURL:**
```bash
curl -X GET "http://localhost:8080/v1/autocomplete?q=goro&limit=5"
```

---

#### 7. `POST /v1/extract`

Fetches a web URL, parses HTML into Markdown, and extracts specific structured fields into a JSON object.

- **Parameters & Bounds**:
  - `url` (string, **required**): Target website URL.
  - `fields` (array of strings, **required**): Schema field names to extract.

**cURL:**
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

Microservice API endpoint (in `agent-service`) for executing hybrid RRF search over the indexed corpus.

- **Parameters & Bounds**:
  - `query` / `q` (string, **required**): Search query.
  - `top_k` (integer, *optional*, default: `5`, bounds: `1` to `100`): Hit count.

**cURL:**
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

Returns OpenAI-compatible function definition tool schemas for integration into AI agent tool-calling loops.

- **Parameters**: None

**cURL:**
```bash
curl -X GET http://localhost:8090/v1/agent/tools
```

---

## 🛡️ Security & Rate Limiting

WebLimbAI implements multi-tier rate limiting and client IP identification:

- **Embedded Light Server (`cmd/agentlimbs-light`)**:
  - Token bucket rate limiter configured at **50 requests/sec** with a burst capacity of **100 requests**.
- **Agent Service (`agent-service`)**:
  - Dynamic sliding-window rate limiter evaluated per client IP and `/24` IPv4 (or `/64` IPv6) subnet:
    - **Local / Enterprise Mode (`EXECUTION_MODE=local`)**: **1,800 req/min** (30 req/sec).
    - **Public Cloud SaaS Mode (`EXECUTION_MODE=cloud`)**: **300 req/min** (5 req/sec).
- **Client IP Determination**:
  - Validates `RemoteAddr` against `TRUSTED_PROXIES` (default `127.0.0.1,::1`). Trusted proxies parse `X-Forwarded-For` right-to-left to prevent IP spoofing attacks.

---

## 🔌 Model Context Protocol (MCP) Integration

Connect WebLimbAI to your AI IDE (Antigravity, Cursor, Claude Desktop) as a stdio tool provider.

### Setup Config (`mcp_config.json`)

```json
{
  "mcpServers": {
    "weblimb": {
      "command": "go",
      "args": ["run", "mcp-server/main.go"]
    }
  }
}
```

### Exact MCP Tool JSON Schemas

#### Tool 1: `agent_limbs_scrape`

```json
{
  "name": "agent_limbs_scrape",
  "description": "Scrape and extract clean Markdown content from a web URL into WebLimbAI search index",
  "inputSchema": {
    "type": "object",
    "properties": {
      "url": {
        "type": "string",
        "description": "Target URL to scrape"
      },
      "mode": {
        "type": "string",
        "description": "Extraction mode",
        "enum": ["clean_rag", "preserve_links", "raw"]
      },
      "ttl_seconds": {
        "type": "integer",
        "description": "Time to live for the scraped document in seconds"
      }
    },
    "required": ["url"]
  }
}
```

#### Tool 2: `agent_limbs_hybrid_search`

```json
{
  "name": "agent_limbs_hybrid_search",
  "description": "Perform hybrid BM25 and vector RAG search over indexed documents",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Search query"
      },
      "limit": {
        "type": "integer",
        "description": "Maximum results count"
      }
    },
    "required": ["query"]
  }
}
```

---

## 📊 Companion Web Dashboard

WebLimbAI includes a companion web dashboard located in `agentlimbs-dashboard` running on **`http://localhost:3001`**.

- **Web Scraping Playground**: Test URL scraping with live Markdown preview, token reduction stats, and custom TTL assignment.
- **Hybrid RRF Search Workbench**: Execute search queries, view BM25 vs Vector scores, and inspect post-RRF candidate rankings.
- **DeepSeek Agentic Reasoner Visualizer**: Track multi-step agent reasoning steps, sub-query decomposition, and inline citations.
- **Corpus & System Metrics**: Monitor memory usage, active indexed document counts, trie vocabulary size, and TTL janitor status.

---

## ⚙️ Environment Variables Table

Configure WebLimbAI using environment variables in `.env` or system shell:

| Variable | Description | Default | Bounds / Allowed Values |
| :--- | :--- | :--- | :--- |
| `PORT` | HTTP server port for `agentlimbs-light` | `8080` | Valid TCP port (e.g. `8080`) |
| `AGENT_PORT` | HTTP server port for `agent-service` | `8090` | Valid TCP port (e.g. `8090`) |
| `DATA_DIR` | Local snapshot directory path for embedded server | `data` | Valid filesystem directory path |
| `EXECUTION_MODE` | Deployment execution profile | `local` | `local`, `distributed`, `cloud`, `embedded` |
| `EMBEDDING_PROVIDER` | Pluggable vector embedding engine | `default` | `default` (128-D Subword), `cohere`, `openai`, `ollama` |
| `COHERE_API_KEY` | API Key when `EMBEDDING_PROVIDER=cohere` | *(Optional)* | Valid Cohere API key string |
| `OPENAI_API_KEY` | API Key when `EMBEDDING_PROVIDER=openai` / LLM fallback | *(Optional)* | Valid OpenAI API key string |
| `OPENAI_BASE_URL` | Base URL endpoint for OpenAI provider | `https://api.openai.com/v1` | Valid URL string |
| `DEEPSEEK_API_KEY` | API Key for DeepSeek agentic search LLM reasoning | *(Optional)* | Valid DeepSeek API key string |
| `DEEPSEEK_BASE_URL` | Base URL endpoint for DeepSeek provider | `https://api.deepseek.com/v1` | Valid URL string |
| `DEEPSEEK_MODEL` | LLM model identifier for agentic search | `deepseek-chat` | Model ID (e.g. `deepseek-chat`, `deepseek-reasoner`) |
| `OLLAMA_HOST` | Host URL when `EMBEDDING_PROVIDER=ollama` | `http://localhost:11434` | Valid HTTP URL |
| `DEFAULT_TTL_SECONDS` | Default document expiration fallback (seconds) | `604800` | Positive integer (default 7 days) |
| `JANITOR_INTERVAL` | Background TTL janitor cleanup frequency | `15m` | Valid duration string (e.g. `15m`, `1h`) |
| `AGENT_API_KEY` | Secret API Key for HTTP endpoints authentication | *(Optional)* | Secret string |
| `TRUSTED_PROXIES` | Comma-separated CIDRs/IPs trusted for rate limit headers | `127.0.0.1,::1` | Comma-separated IPs / CIDRs |
| `DATABASE_URL` | PostgreSQL connection string (falls back to JSON file snapshot) | *(Optional)* | `postgres://user:pass@host:port/dbname` |
| `KAFKA_BROKERS` | Comma-separated Kafka broker addresses for event indexing | *(Optional)* | `host1:9092,host2:9092` |

---

## 📄 OpenAPI Specification & SDE Corpus Seed CLI

### OpenAPI 3.0 Specification (`openapi.json`)

The monorepo contains a complete OpenAPI 3.0 REST API specification in [`openapi.json`](file:///Users/anishraj/Desktop/Placement_Projects/crawler-monorepo/openapi.json). You can import `openapi.json` into Postman, Swagger UI, Insomnia, or client SDK generators.

### SDE Corpus Seed Engine (`cmd/seed_sde_corpus`)

WebLimbAI includes a batch corpus seed CLI tool to populate the vector, trie, and BM25 index engines with **1,000+ Software Development Engineering (SDE) technical documents** across 10 core domains (Data Structures, System Design, Storage Engines, OS Kernel, Computer Networking, Go Concurrency, Cloud/K8s, Object Design Patterns, Cryptography/Security, ML Infra & RAG).

Run the seed script locally:

```bash
go run cmd/seed_sde_corpus/main.go
```

**Output Statistics:**
- Total Documents Ingested: **1,000 pages**
- Ingestion Throughput: **~4,000+ docs/sec**
- In-memory Vocabulary Size: **~1,400+ unique terms**

---

## 🧪 Running Tests & Verification

Run the full workspace unit and integration test suite across all packages:

```bash
go test -v ./...
```

---

## 🔍 Architectural Scope, Design Philosophy & Limitations

WebLimbAI is designed around the **Unix Philosophy for AI Agents**: *Provide a single-binary, deterministic, sub-100ms sensory limb for agents without coupling them to heavy external runtimes.*

### 1. High-Speed PDF Text Extraction vs. Image OCR
- **Native Text Layer**: WebLimbAI decodes PostScript text streams, Type 1 font difference encodings, kerning displacements, and decomposed ligatures natively in Go in **<100ms**.
- **Embedded Raster Figures (Documented Limitation)**: Text baked inside embedded bitmap graphics (e.g. flowchart box labels in architecture diagrams or rasterized charts) is intentionally not parsed via traditional OCR in the fast path.
- **Why No Heavy OCR / Vision Ingestion?**:
  1. **Latency & Cost**: Calling multimodal vision models on 15 figures per paper adds 5–10 seconds of latency and external API token costs to every scrape.
  2. **Zero CGo / Runtime Bloat**: Traditional OCR (Tesseract) requires heavy system C-libraries, breaking the single-binary zero-dependency deployment model.
  3. **Agentic Separation of Concerns**: Consuming agents (such as Claude, GPT-4, or Gemini) already possess world-class vision capabilities. WebLimbAI keeps the ingestion layer blazing fast and lightweight, leaving multimodal image reasoning to the calling agent on demand.

---

## ⚡ High-Scale Performance Benchmark (500,000+ Documents)

WebLimbAI includes dedicated benchmarking suites in `scripts/benchmarks/` to measure ingestion throughput, memory overhead, and search latency across large-scale corpora.

### 500,000-Document Benchmark Results

| Metric | Result | Notes |
| :--- | :--- | :--- |
| **Ingestion Throughput** | **93,542 docs/second** | Ingests 500k pages in **5.34 seconds** |
| **Active Heap Memory** | **1.27 GB RAM** | ~**2.61 KB per document** (postings, trie, 64 shards, vectors) |
| **p50 Search Latency (Median)** | **2.44 ms** | Sub-3ms hybrid BM25 + Vector + RRF ranking |
| **p90 Search Latency** | **3.38 ms** | Fast, responsive under load |
| **p95 Search Latency** | **4.00 ms** | Ultra-consistent percentile bounds |
| **p99 Search Latency** | **12.80 ms** | Guaranteed sub-15ms tail latency |
| **Kafka Offset Watermarking** | **1,302,192 msgs/sec** | 500k messages tracked across 8 partitions with 0 data loss |

---

## 🖥️ CPU, Concurrency & Resource Configuration

### Default CPU Behavior
In all production binaries (`lightlimbs`, `agent-service`, `search-service`, `indexer-service`, `mcp-server`), **Go automatically utilizes all available CPU cores on the host machine** (e.g. 8, 16, 32, 64 cores) without artificial caps.

### How to Configure Resource Limits

You can adjust CPU allocation and concurrency dynamically across environments:

#### 1. Via Environment Variable (`GOMAXPROCS`)
Control the number of operating system threads executing Go code:

```bash
# Low-impact mode for local laptops / background sidecars:
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

#### 3. Via Kubernetes Pod Resource Limits
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

## 🏛️ Deployment Architecture: When to Use Which Mode

```mermaid
graph LR
    subgraph SingleBinary ["LightLimbs Mode (Embedded & Developer Workstation)"]
        L1["lightlimbs\n(Single Binary, ~25MB RAM)"]
        M1["agent-limbs-mcp\n(Cursor / Claude Desktop)"]
    end

    subgraph Distributed ["PowerLimbs Mode (Distributed Raft & Kafka Cluster)"]
        A1["agent-service\n(Scraping)"] --> K1[("Kafka Topic")]
        K1 --> I1["indexer-service\n(Indexing)"]
        I1 --> S1["search-service\n(Search)"]
    end
```

| Dimension | **LightLimbs Mode (`lightlimbs` / MCP)** | **PowerLimbs Cluster (`agent` + `indexer` + `search` + `raft`)** |
| :--- | :--- | :--- |
| **Best For** | Cursor / Claude / IDE tool calls, Local RAG, Developer Workstations, Startups (<500k docs) | Enterprise multi-tenant crawling platforms (Tavily/Perplexity-style, >500k docs) |
| **Operational Overhead** | **Zero** (1 static binary, no Docker, no external services) | **Cluster** (Partitioned Raft, Kafka KRaft, PostgreSQL, Redis) |
| **Search Latency** | **< 1 ms** (in-process memory lookup) | **~2–4 ms** (network hop / scatter-gather coordinator) |
| **Dependencies** | None (embedded JSON snapshot fallback) | PostgreSQL (`pgxpool`), Kafka (KRaft), Redis |

---

## 📄 License

Distributed under the MIT License. See `LICENSE` for details.
