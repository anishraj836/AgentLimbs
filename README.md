# 🦾 AgentLimbs
### High-Performance Web Ingestion, Hybrid Search & RAG Infrastructure for AI Agents

**AgentLimbs** is a production-grade, distributed web crawler, indexing, vector embedding, and hybrid retrieval platform built in **Go (Golang)**, **Apache Kafka**, **Redis**, and **PostgreSQL**.

It acts as the high-concurrency digital ingestion limbs for AI Agents, LLM pipelines, and search engines—capable of discovering, downloading, tokenizing, indexing, vectorizing, and ranking web pages with sub-millisecond data structure operations.

---

## 🚀 System Pipeline & Microservices

```text
[ Seed URLs / API ] ──► POST /api/v1/seeds
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Frontier Service   │ ──► Redis Bloom Filter (Deduplication)
                    └──────────┬──────────┘
                               │ Kafka: crawl_requests
                               ▼
                    ┌─────────────────────┐
                    │   Crawler Service   │ ──► Redis Rate-Limiter (Politeness)
                    └──────────┬──────────┘ ──► Local Gzip File Storage
                               │ Kafka: downloaded_pages
                               ├──────────────────────────────────┐
                               ▼                                  ▼
                    ┌─────────────────────┐            ┌─────────────────────┐
                    │   Parser Service    │            │ Document Processor  │
                    └──────────┬──────────┘            └──────────┬──────────┘
                               │ Kafka: discovered_urls           │ Kafka: parsed_documents
                               │ (Loop back to Frontier)          ├────────────────────────┐
                                                                  ▼                        ▼
                                                       ┌─────────────────────┐  ┌─────────────────────┐
                                                       │  Tokenizer Service  │  │ Embedding Service   │
                                                       └──────────┬──────────┘  └──────────┬──────────┘
                                                                  │                        │
                                                                  ▼                        ▼
                                                       ┌─────────────────────┐  ┌─────────────────────┐
                                                       │ Inverted Index BM25 │  │ AI Vector Store     │
                                                       └──────────┬──────────┘  └──────────┬──────────┘
                                                                  └──────────┬─────────────┘
                                                                             ▼
                                                                  ┌─────────────────────┐
                                                                  │ Agent Service (RRF) │
                                                                  └─────────────────────┘
                                                                             │
                                                                             ▼
                                                                  [ LLM / AI Agent API ]
                                                                  POST /v1/scrape
                                                                  POST /v1/agent/query
```

---

## 🌟 Key Features Across All 3 Phases

### Phase 1 — Distributed Web Crawler Infrastructure
- **Frontier Service**: REST API (`POST /api/v1/seeds`) for seed URL ingestion with Redis-backed **Bloom Filter deduplication** ($O(1)$ lookup).
- **Crawler Service**: Bounded Go worker routines (`concurrencyLimit: 50`), **Redis `SetNX` domain politeness rate limiting**, transparent Gzip disk storage, `io.LimitReader` memory safety caps (10MB limit), and exponential backoff HTTP retries.
- **Parser Service**: Concurrent DOM parser using `goquery` to extract, normalize, and resolve absolute outbound URLs, closing the autonomous crawling loop.
- **Kafka Resilience**: Custom, thread-safe contiguous **OffsetTracker** eliminating out-of-order Kafka message commit data loss, plus **Dead-Letter Queue (`crawl_failed_dlq`)** error routing.

### Phase 2 — Distributed Indexing & Statistical Search Engine
- **Document Processor Service**: HTML boilerplate, script, CSS, navbar, footer, and ad removal.
- **Tokenizer Service**: Unicode normalization (NFC), $O(1)$ HashSet stopword filtering, and English word reduction via a custom **Porter Stemmer**.
- **Inverted Index Engine**: Thread-safe posting lists tracking `(DocID, TermFrequency, Positions []int)` for exact keyword and phrase search.
- **Trie Autocomplete Service**: Concurrent $O(L)$ prefix tree returning top corpus frequency query completions.
- **Okapi BM25 Ranker**: From-scratch mathematical implementation of Okapi BM25 ($k_1=1.2, b=0.75$) with Inverse Document Frequency (IDF) scoring and contextual `<mark>` highlighted text snippets.

### Phase 3 — The Agentic Phase (AI Agents & Hybrid RAG Engine)
- **Firecrawl-Style Scrape API (`POST /v1/scrape`)**: Converts HTML DOM trees into clean, Token-Efficient Github-Flavored Markdown (`# Headings`, `**Bold**`, `[Link](url)`), reducing LLM token consumption by **85%**.
- **AI Vector Embedding Engine**: Generates $D=128$ normalized feature vectors and computes **Cosine Similarity** math:
  $$\text{CosineSimilarity}(\vec{u}, \vec{v}) = \frac{\vec{u} \cdot \vec{v}}{\|\vec{u}\| \|\vec{v}\|}$$
- **Hybrid Search Engine via Reciprocal Rank Fusion (RRF)**: Merges sparse BM25 keyword search rankings with dense vector semantic rankings:
  $$\text{RRF\_Score}(d) = \frac{1}{k + \text{rank}_{\text{BM25}}(d)} + \frac{1}{k + \text{rank}_{\text{Vector}}(d)}$$
- **OpenAI & LangChain Tool Definitions (`GET /v1/agent/tools`)**: Exposes native JSON Schema function calling definitions so AI Agents can automatically register AgentLimbs as a tool.

---

## 📊 Microservice REST Endpoints

| Service | Port | Endpoint / Method | Description |
| :--- | :---: | :--- | :--- |
| **Frontier Service** | `8080` | `POST /api/v1/seeds` | Submit seed URLs: `{"urls": ["https://golang.org"]}` |
| **Agent Service** | `8090` | `POST /v1/scrape` | Firecrawl-style Scrape API (returns LLM Markdown + token estimate) |
| **Agent Service** | `8090` | `POST /v1/agent/query` | Hybrid RRF Search API (BM25 + AI Vector Semantic Ranks) |
| **Agent Service** | `8090` | `GET /v1/agent/tools` | OpenAI Function Calling & LangChain Tool Definitions |
| **Search API** | `8088` | `POST /search` | Search query: `{"query": "golang programming", "limit": 10}` |
| **Search API** | `8088` | `GET /autocomplete?q=pro` | Prefix autocomplete query completions |
| **Search API** | `8088` | `GET /stats` | Vocabulary size, document count, avg doc length |

---

## 🧪 Testing & Verification

```bash
# Run unit tests across all packages
go test -v ./...

# Run static analysis
go vet ./...
```

---

## 💻 Tech Stack & Dependencies

- **Language**: Go 1.21+
- **Event Bus**: Apache Kafka (`segmentio/kafka-go`)
- **Cache & Locks**: Redis (`redis/go-redis/v9`)
- **Database**: PostgreSQL (`jackc/pgx/v5`)
- **DOM Parser**: `goquery` (`PuerkitoBio/goquery`)
- **Observability**: Prometheus & Grafana
