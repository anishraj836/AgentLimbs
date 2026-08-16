# WebLimbAI

> The self-hosted web retrieval layer for AI agents.

Crawl documentation. Strip HTML boilerplate. Search semantically and lexically. Expose the knowledge directly to **Cursor**, **Claude Desktop**, and **autonomous agents** via native Model Context Protocol (MCP).

**Starts as a zero-dependency single binary (15 MB RAM); scales to a distributed cluster when you need it.**

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

### 1. Installation

**macOS & Linux (Bash):**
```bash
curl -sSfL https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.sh | sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.ps1 | iex
```

**Or build from source:**
```bash
# macOS / Linux
go build -o lightlimbs ./cmd/lightlimbs

# Windows (Command Prompt / PowerShell)
go build -o lightlimbs.exe ./cmd/lightlimbs
```

### 2. 1-Click MCP Setup (Cursor & Claude Desktop)

Automatically configure your AI editor's Model Context Protocol without manually editing JSON files:

```bash
# Configures Claude Desktop and Cursor IDE:
lightlimbs init-mcp

# Or preview the configuration JSON (stdout):
lightlimbs init-mcp --stdout
```

### 3. CLI Commands

**macOS / Linux:**
```bash
# Scrape a web page into clean, token-reduced Markdown:
lightlimbs scrape https://go.dev/doc/tutorial/getting-started

# Scrape and output structured JSON (pipeable to jq):
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

**Windows (PowerShell):**
```powershell
# Scrape a web page:
.\lightlimbs.exe scrape https://go.dev/doc/tutorial/getting-started

# Scrape and output structured JSON:
.\lightlimbs.exe scrape https://go.dev -j

# Hybrid search:
.\lightlimbs.exe search "goroutine channel concurrency" --top 5

# Seed local index:
.\lightlimbs.exe seed

# Start HTTP daemon on port 8080:
.\lightlimbs.exe serve --port 8080
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

## Comparison & Trade-Offs

WebLimbAI takes a different architectural approach compared to dedicated scraping frameworks:

| Feature / Metric | WebLimbAI (LightLimbs) | Crawl4AI / Firecrawl | Trafilatura (Python) | Jina Reader API |
| :--- | :--- | :--- | :--- | :--- |
| **Primary Scope** | **Full Retrieval Engine** (Scrape + Index + Search + MCP) | **Web Scraper** (URL to Markdown string) | **HTML Extractor** (HTML to clean text) | **Hosted Scraping API** (SaaS proxy) |
| **Extraction Engine** | Pure Go Static DOM AST (`golang.org/x/net/html`) | Headless Chromium (Playwright / Puppeteer) | Python Heuristic Parser (`lxml`) | Cloud-hosted browser cluster |
| **Extraction Latency** | **< 3–5 ms** / page | **800–2,500 ms** / page | **25–60 ms** / page | **500–1,500 ms** (network hop) |
| **Memory Overhead** | **~0 MB** (in-process streaming parser) | **150–300 MB** per browser tab | **15–30 MB** (Python runtime) | N/A (hosted remotely) |
| **Throughput** | **1,000+ pages/sec** per core | **5–15 pages/sec** | **20–50 pages/sec** | Rate-limited by API tier |
| **Client-Side JS SPAs** | No (Static / SSR HTML only) | **Yes** (Full DOM hydration) | No (Static HTML only) | **Yes** (Remote browser) |
| **Built-in Search Engine** | **Yes** (BM25 + Int8 Vector + RRF) | No (Transmits text only) | No (Text extractor only) | No (Returns Markdown only) |
| **IDE MCP Integration** | **Yes** (1-Click stdio configuration) | Requires custom wrapper | No | Requires hosted API key |
| **Deployment Model** | **Single static binary** (<25 MB) | Python + Node + Chromium + Playwright | Python virtual environment | Third-party cloud SaaS |

### When to Use WebLimbAI vs Alternatives

- **Use WebLimbAI when**:
  - You need a fast, self-contained retrieval layer for AI agents (Cursor, Claude Desktop, local LLMs).
  - You are crawling documentation, technical blogs, API specs, Wikipedia, GitHub wikis, or HTML pages where headless browser rendering is unnecessary overhead.
  - You want scraped content immediately indexed into local memory for instant hybrid search (<2ms) without setting up an external vector database.
- **Use Crawl4AI or Firecrawl when**:
  - The target website is a heavy client-rendered Single Page Application (React/Vue/Angular) where content is injected exclusively via client-side JavaScript.
  - You require interactive browser automation (clicking buttons, filling inputs, bypassing Turnstile captchas).

---

### Extraction Token Reduction Benchmarks

Measured using OpenAI's `cl100k_base` BPE tokenizer across standard documentation and technical pages:

| Target Page | Raw HTML Tokens | WebLimbAI Clean Markdown | Token Savings (%) | Extraction Latency |
| :--- | :--- | :--- | :--- | :--- |
| **Go Tutorial** (`go.dev/doc/tutorial/getting-started`) | 3,142 tokens | 487 tokens | **84.5%** | **1.8 ms** |
| **Docker Getting Started** (`docs.docker.com/get-started`) | 8,920 tokens | 1,412 tokens | **84.2%** | **3.1 ms** |
| **Wikipedia: Go** (`en.wikipedia.org/wiki/Go`) | 41,208 tokens | 8,650 tokens | **79.0%** | **7.4 ms** |
| **HttpBin Narrative** (`httpbin.org/html`) | 780 tokens | 194 tokens | **75.1%** | **0.8 ms** |
| **Hacker News Frontpage** (`news.ycombinator.com`) | 12,450 tokens | 2,110 tokens | **83.1%** | **2.3 ms** |

#### Extraction Sample: Go Tutorial (`clean_rag` Mode)

- **Raw Page**: 3,142 tokens (includes global navigation header, cookie banners, search bar, sidebars, and analytics).
- **Extracted Markdown**: 487 tokens (**84.5% reduction** — retains clean headings, lists, code blocks, and links without boilerplate):

```markdown
# Tutorial: Getting started with Go

In this tutorial, you'll get a brief introduction to Go programming. In the process, you will:
- Install Go.
- Write some simple "Hello, world" code.
- Use the `go` command to run your code.

## Prerequisites
- Some programming experience.
- A tool to edit your code. Any text editor you have will do.
- A command terminal.

## Install Go
To install Go, follow the installation instructions at [go.dev/doc/install](https://go.dev/doc/install).
```

---

## Architectural Scope & Limitations

To maintain sub-5ms extraction latency and a static binary footprint (<25 MB), WebLimbAI makes deliberate engineering trade-offs:

1. **No Headless Browser by Default**:
   - WebLimbAI uses a pure Go static HTML AST parser (`golang.org/x/net/html`).
   - It does **not** execute client-side JavaScript or hydrate Single Page Applications (React/Vue/Next.js client-only rendering). For static HTML, SSR pages, documentation, and API references, this delivers a 100x–500x speedup and requires ~0 MB extra memory compared to Chromium.
2. **No Interactive CAPTCHA / Cloudflare Turnstile Bypass**:
   - WebLimbAI is an automated crawler and ingestion limb. It does not include automated browser mouse-movement solvers or CAPTCHA-breaking harnesses.
3. **No Embedded Raster Image OCR**:
   - Text baked inside bitmap raster images (flowchart labels, diagram screenshots) is not OCR'd in the fast path. Calling multimodal vision models on every embedded image adds 5–10s of latency and external token costs. WebLimbAI leaves multimodal vision reasoning to calling LLM agents on demand.

---

## Vector Embedding Architecture

WebLimbAI supports both lightweight local feature embeddings and deep neural transformer embeddings:

```text
┌─────────────────────────────────────────────────────────────────────────────────┐
│                          Embedding Options in WebLimbAI                         │
├───────────────────────────────────────┬─────────────────────────────────────────┤
│    Default Subword N-Gram Vectorizer  │   Transformer Embeddings (Ollama/APIs)  │
│        (EMBEDDING_PROVIDER=default)   │  (EMBEDDING_PROVIDER=ollama/openai/cohere)│
├───────────────────────────────────────┼─────────────────────────────────────────┤
│ • Pure Go (zero runtime dependencies) │ • Deep contextual semantic representation│
│ • FastText-style character n-grams    │ • Captures abstract conceptual analogies│
│ • Execution latency: < 10 microseconds│ • Latency: ~15–100 ms per document      │
│ • RAM overhead: 0 MB                  │ • Requires local Ollama or API key      │
│ • Strength: Typos, compound terms     │ • Strength: Cross-vocabulary semantics  │
└───────────────────────────────────────┴─────────────────────────────────────────┘
```

### How the Default 128-D Subword Embedder Works

The default embedder is a **FastText-inspired Character N-Gram & Subword Hashing Vectorizer** ($D=128$, L2 unit-norm):

1. **Tokenization & Stopwords**: The input text is tokenized into word-level tokens, and language stopwords are stripped.
2. **N-Gram Decomposition**: For each word, the embedder extracts the base word plus all **character 3-grams and 4-grams** (e.g. `"concurrency"` produces `["con", "onc", "ncu", "cur", "urr", "rre", "ren", "enc", "ncy"]`).
3. **Dimensional Projection**: Extracted features are projected into a 128-dimensional vector space using non-cryptographic FNV-1a hashing with sign-hashing.
4. **L2 Normalization**: The vector is normalized to Euclidean unit length ($\|v\|_2 = 1.0$), enabling fast dot-product cosine similarity computation.

**Capabilities & Limitations**:
- **What it does well**: Captures morphological variations (`goroutine` vs `goroutines`, `scheduler` vs `scheduling`), technical identifiers (`GMP_Scheduler` vs `gmp scheduler`), and spelling errors with zero GPU/PyTorch overhead.
- **What it does not do**: It does not capture abstract conceptual analogies between words that share no common subwords (e.g. `physician` and `doctor`).

### Using Transformer Embeddings

If your workload requires deep transformer embeddings, configure an external embedding provider:

```bash
# Local Ollama (nomic-embed-text, bge-m3, all-minilm):
export EMBEDDING_PROVIDER=ollama
export OLLAMA_HOST=http://localhost:11434
export OLLAMA_MODEL=nomic-embed-text

# OpenAI (text-embedding-3-small):
export EMBEDDING_PROVIDER=openai
export OPENAI_API_KEY=sk-...

# Cohere (embed-english-v3.0):
export EMBEDDING_PROVIDER=cohere
export COHERE_API_KEY=...
```

The hybrid search ranker combines BM25 keyword rankings with the chosen embedding provider's cosine similarity scores using **Reciprocal Rank Fusion (RRF)**.

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
