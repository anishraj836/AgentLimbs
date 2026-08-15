package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
)

type SDEDomain struct {
	Category string
	Topics   []string
	Template string
}

func main() {
	fmt.Println("🚀 Initializing 1,000+ SDE Corpus Batch Ingestion Engine...")

	storage.InitDB(os.Getenv("DATABASE_URL"))
	defer storage.CloseDB()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	domains := []SDEDomain{
		{
			Category: "Data Structures & Algorithms",
			Topics: []string{
				"Dynamic Programming Memoization", "Red-Black Tree Self Balancing", "Trie Prefix Tree Autocomplete",
				"Segment Tree Range Query", "Disjoint Set Union Find Path Compression", "Monotonic Stack Next Greater Element",
				"Sliding Window Maximum", "Fenwick Tree Binary Indexed Tree", "A Star Pathfinding Algorithm", "Dijkstra Shortest Path Single Source",
				"Floyd Warshall All Pairs Shortest Path", "Tarjan Strongly Connected Components", "Kruskal Minimum Spanning Tree",
				"Prim Minimum Spanning Tree", "Topological Sort Directed Acyclic Graph", "KMP Knuth Morris Pratt String Matching",
				"Rabin Karp Rolling Hash Search", "B Tree Storage Node Balancing", "B Plus Tree Leaf Linked List", "AVL Tree Height Balance",
				"Skip List Concurrent Skip List", "LRU Cache Least Recently Used Doubly Linked List", "LFU Cache Least Frequently Used",
				"Hash Map Collision Resolution Chaining Open Addressing", "Bloom Filter Probabilistic Set Membership",
				"Count Min Sketch Frequency Estimation", "HyperLogLog Cardinality Estimation", "Spatial Index R Tree QuadTree",
				"Suffix Tree Suffix Array", "QuickSort Partitioning Median of Three", "MergeSort Stable Counting Inversions",
				"HeapSort Priority Queue Binary Heap", "Radix Sort LSD MSD Counting", "Bucket Sort Distribution",
				"Binary Search Lower Bound Upper Bound", "Ternary Search Unimodal Function", "Two Pointers Opposite Direction Same Direction",
				"Backtracking N Queens Sudoku Solver", "Greedy Algorithm Fractional Knapsack", "Branch and Bound Traveling Salesperson",
			},
			Template: "Software Development Engineering DSA Topic: %s. Provides O(1) or O(log N) efficiency for time and space optimization, memory layout management, and algorithmic complexity tradeoffs.",
		},
		{
			Category: "System Design & Distributed Systems",
			Topics: []string{
				"Consistent Hashing Virtual Nodes Ring", "CAP Theorem Consistency Availability Partition Tolerance",
				"Raft Consensus Protocol Leader Election Log Replication", "Paxos Distributed Consensus Protocol",
				"Vector Clocks Causal Ordering Logical Time", "Rate Limiter Token Bucket Leaky Bucket Fixed Window",
				"Distributed Locking Redis Redlock Zookeeper", "Message Broker Apache Kafka Consumer Groups Partitions",
				"Circuit Breaker Pattern State Transition Retry Exponential Backoff", "Event Sourcing CQRS Command Query Responsibility Segregation",
				"Distributed Tracing OpenTelemetry Jaeger Context Propagation", "API Gateway Reverse Proxy Nginx Envoy Routing",
				"Distributed Transaction Two Phase Commit 2PC Saga Pattern", "Distributed Cache Redis Cluster Eviction Policies",
				"Load Balancer Round Robin Weighted Least Connections", "Database Sharding Horizontal Partitioning Key Hash",
				"Read Replicas Master Slave Replication Lag", "Heartbeat Liveness Health Checks Failure Detection", "Gossip Protocol Cluster Membership SWIM",
				"Idempotency Key Deduplication Replay Protection", "Bulkhead Isolation Thread Pool Separation",
				"Write Through Cache Write Back Cache Write Around", "CDN Content Delivery Network Edge Caching Anycast",
				"Distributed File System HDFS Ceph Blob Storage", "Service Discovery Consul Eureka DNS Based",
				"SLA SLO SLI Service Level Agreement Availability Metrics", "Log Aggregation ELK Stack Fluentd Loki",
				"Database Connection Pooling PgBouncer HikariCP", "Distributed ID Generator Snowflake UUID ULID",
				"Graceful Degradation Fallback Response Rate Throttling",
			},
			Template: "High-Scalability System Design Topic: %s. Architected for 99.999%% uptime, horizontal scalability, fault tolerance, microservice decoupling, and low-latency payload throughput.",
		},
		{
			Category: "Databases & Storage Engines",
			Topics: []string{
				"LSM Tree Log Structured Merge Tree SSTable MemTable", "B Plus Tree Storage Engine InnoDB WiredTiger",
				"WAL Write Ahead Logging Crash Recovery Journaling", "MVCC Multi Version Concurrency Control Snapshot Isolation",
				"PostgreSQL Indexing BTree GIN GiST BRIN Indexes", "Database Isolation Levels Read Uncommitted Read Committed Repeatable Read Serializable",
				"ACID Transactions Atomicity Consistency Isolation Durability", "Database Sharding Range Hash Directory Based",
				"Redis In Memory Data Structures Hash Set Sorted Set Bitmap", "NoSQL Document Store MongoDB Cassandra Key Value",
				"Graph Database Neo4j Cypher Property Graph", "Columnar Database ClickHouse Apache Parquet OLAP Analytics",
				"Query Optimization EXPLAIN ANALYZE Cost Based Optimizer", "Database Locking Shared Lock Exclusive Lock Intent Locks Row Level",
				"Deadlock Detection Lock Wait Timeout Wait For Graph", "Vector Database HNSW Faiss Milvus Embeddings",
				"Connection Pooling Max Open Connections Max Idle Lifetime", "Database Migration Schema Versioning Liquibase Flyway",
				"Replication Sync Async Semi Sync Replication", "CDC Change Data Capture Debezium Event Streaming",
			},
			Template: "Database & Storage Systems Architecture: %s. Focuses on query execution plans, indexing strategies, transaction ACID isolation, write amplification, and disk IOPS performance.",
		},
		{
			Category: "Operating Systems & Low-Level Engineering",
			Topics: []string{
				"Process vs Thread Memory Layout Heap Stack Text Data", "Context Switching CPU Registers Translation Lookaside Buffer TLB",
				"Page Replacement Algorithms LRU Clock Second Chance FIFO", "Virtual Memory Page Tables Paging Segmentation Page Fault",
				"Inodes File Descriptors VFS Virtual File System", "Memory Allocator malloc free jemalloc tcmalloc Slab Allocator",
				"Mutex Semaphore Condition Variable Spinlock Mutex Contention", "Futex Fast Userspace Mutex Linux Kernel Locking",
				"Inter Process Communication Shared Memory Pipe Unix Domain Socket Signal", "Non Blocking IO epoll kqueue IO Multiplexing Select Poll",
				"Asynchronous IO io_uring Ring Buffers Completion Queue", "Linux System Calls sys_enter sys_exit Kernel Mode User Mode",
				"CPU Cache Lines L1 L2 L3 Cache False Sharing Cache Coherence MESI", "Zero Copy I O sendfile splice DMA Direct Memory Access",
				"Signal Handling SIGTERM SIGKILL SIGSEGV Signal Masks", "Cgroups v2 Resource Limits CPU Memory IO Quotas",
				"Namespaces PID Mount Network UTS IPC User Container Isolation", "Thread Synchronization Memory Barrier Atomic Load Store Acquire Release",
				"Process Scheduling O(1) Scheduler Completely Fair Scheduler CFS", "Memory Mapped Files mmap msync Page Cache Sync",
			},
			Template: "Operating System & Systems Kernel Topic: %s. Covers kernel primitives, CPU caching, memory management, low-level concurrency control, and system call throughput.",
		},
		{
			Category: "Networking & Distributed Protocols",
			Topics: []string{
				"TCP Three Way Handshake SYN SYN ACK ACK Congestion Control", "HTTP 2 Multiplexing Server Push Binary Framing Header Compression",
				"HTTP 3 QUIC Protocol UDP Fast Handshake Connection Migration", "TLS 1.3 Handshake Key Exchange Forward Secrecy Cipher Suites",
				"DNS Resolution Recursive Iterative A AAAA CNAME MX Records", "BGP Border Gateway Protocol Autonomous Systems Path Vector",
				"Subnetting CIDR IPv4 IPv6 Routing Tables Default Gateway", "Socket Programming Non Blocking Sockets epoll kqueue Event Loop",
				"WebSockets Full Duplex Bidirectional Upgrade Handshake", "gRPC Protocol Buffers HTTP 2 Streaming RPC Serialization",
				"NAT Network Address Translation STUN TURN ICE Hole Punching", "UDP Datagram Connectionless Low Latency Packet Loss",
				"TCP Window Size Sliding Window Window Scaling Flow Control", "SSL TLS Certificates CA Public Key Infrastructure PKI",
				"Reverse Proxy Nginx Proxy Pass Host Header Buffering", "ALPN Application Layer Protocol Negotiation TLS Extension",
				"Keep Alive Persistent Connections Connection Reuse Timeout", "CORS Cross Origin Resource Sharing Preflight Flight Headers",
				"SSRF Server Side Request Forgery Defense Egress Filtering", "Load Balancing Layer 4 TCP vs Layer 7 HTTP Reverse Proxying",
			},
			Template: "Computer Networking & Protocol Engineering: %s. Optimizes packet transmission, handshake latency, protocol frame parsing, network security, and socket IO concurrency.",
		},
		{
			Category: "Go Engineering & Concurrency Systems",
			Topics: []string{
				"Go GMP Scheduler Goroutine M OS Thread P Processor Work Stealing", "Go Channels Buffered Unbuffered Select Case Deadlock Detection",
				"Go Garbage Collector Concurrent Mark Sweep Tri Color Abstraction", "Go Memory Management Small Large Allocations Span Arena mcache mcentral",
				"Go Mutex sync.Mutex sync.RWMutex Starvation Mode Normal Mode", "Go Atomic Operations sync/atomic CompareAndSwap Memory Ordering",
				"Go Escape Analysis Stack vs Heap Allocation Pointer Indirection", "Go Interface Dispatch Interface Table itab Dynamic Method Calls",
				"Go Context Cancellation Timeout Deadline Values Propagation", "Go Singleflight Coalescing Concurrent Duplicate Calls",
				"Go Sync Pool Reuse Garbage Collector Impact Memory Recycling", "Go Slices Header Pointer Length Capacity Resizing Growth",
				"Go Maps Hash Table Bucket Overflow Buckets Eviction Rehash", "Go Defer Statement Performance Cost Return Values Modification",
				"Go Error Handling Panic Recover Custom Error Wrapping", "Go Reflection reflect.Type reflect.Value Performance Impact",
				"Go Generics Type Parameters Type Constraints Performance Compiler", "Go Testing Benchmarks Allocations Profiling pprof",
				"Go Modules go.mod Direct Indirect Dependencies Vendor", "Go Build Tags Conditional Compilation Cross Compilation GOOS GOARCH",
			},
			Template: "Go & High-Concurrency Systems Engineering: %s. Explores Go runtime internals, memory allocation, goroutine scheduler mechanics, channel synchronization, and zero-allocation performance.",
		},
		{
			Category: "Cloud Infrastructure, Kubernetes & DevOps",
			Topics: []string{
				"Kubernetes Pod Scheduling Affinity Anti Affinity NodeSelector Taints Tolerations", "Docker Container Namespaces Cgroups OverlayFS Image Layers",
				"Kafka Partitioning Key Hashing Log Segment Offsets Consumer Groups", "Redis Cluster Hash Slots Sharding Resharding Failover Sentinel",
				"Prometheus Metrics Counter Gauge Histogram Summary Scrape Targets", "Grafana Dashboard Visualization Alerting Metrics Observability",
				"Terraform Infrastructure as Code Provider State File HCL Declarative", "Envoy Proxy Service Mesh Sidecar mTLS Traffic Splitting Filter Chain", "CI CD Pipeline Github Actions Jenkins GitLab CI Docker Build Test Deploy",
				"Horizontal Pod Autoscaler HPA Metrics Server CPU Memory Custom Metrics", "Helm Chart Templates Values Release Rollback Package Manager",
				"Ingress Controller Nginx Traefik Routing Host Rules TLS Termination", "Container Registry Docker Hub ECR GCR Artifact Registry Scans",
				"StatefulSet Persistent Volume Claim PVC Dynamic Provisioning", "Service ClusterIP NodePort LoadBalancer Headless Service",
				"Network Policy Calico Flannel Pod to Pod Egress Ingress Rules", "Chaos Engineering Litmus Chaos Monkey Resiliency Testing",
				"Zero Downtime Deployment Rolling Update Blue Green Canary Release", "Secret Management Vault Kubernetes Secrets Sealed Secrets KMS",
				"Log Aggregation Vector Fluentbit ElasticSearch Kibana OpenSearch",
			},
			Template: "Cloud Native Infrastructure & DevOps Architecture: %s. Provides patterns for container orchestration, automated scaling, continuous delivery, metrics observability, and fault containment.",
		},
		{
			Category: "Object-Oriented Design & System Patterns",
			Topics: []string{
				"SOLID Principles Single Responsibility Open Closed Liskov Interface Segregation Dependency Inversion",
				"Factory Pattern Abstract Factory Object Instantiation Encapsulation", "Strategy Pattern Interface Interchangeable Algorithms Runtime Swap",
				"Observer Pattern Event Driven Publisher Subscriber Listener", "Decorator Pattern Dynamic Behavior Composition Wrapper Class",
				"Singleton Pattern Thread Safe Lazy Initialization Double Checked Locking", "Repository Pattern Data Access Abstraction Persistence Agnostic",
				"Dependency Injection IoC Inversion of Control Container Constructor Injection", "Domain Driven Design DDD Bounded Context Aggregate Root Entity Value Object",
				"Clean Architecture Hexagonal Architecture Ports and Adapters Layer Separation", "Command Pattern Encapsulate Request Undo Redo Queue Execution",
				"State Pattern State Transition Object Behavior Change", "Adapter Pattern Interface Compatibility Legacy Code Wrapper",
				"Facade Pattern Unified Simplified Interface Subsystem", "Proxy Pattern Virtual Proxy Protection Proxy Caching Proxy",
				"Template Method Pattern Algorithm Skeleton Subclass Override", "Chain of Responsibility Pattern Request Handling Pipeline Handler",
				"Flyweight Pattern Memory Savings Shared State Extrinsic State", "Bridge Pattern Decouple Abstraction Implementation",
				"Builder Pattern Fluent Interface Stepwise Complex Object Construction",
			},
			Template: "Software Design Patterns & Object Architecture: %s. Focuses on modular software design, maintainable codebases, loose coupling, high cohesion, and extensible class hierarchies.",
		},
		{
			Category: "Security, Authentication & Cryptography",
			Topics: []string{
				"OAuth 2.0 Authorization Code Flow Access Token Refresh Token Scopes", "OpenID Connect OIDC Identity Layer ID Token JWT Claims Verification",
				"AES Symmetric Encryption AES GCM Authenticated Encryption Nonce", "RSA Public Key Asymmetric Cryptography Key Pair Signatures",
				"Password Hashing Argon2id bcrypt PBKDF2 Salt Work Factor", "SHA 256 Cryptographic Hash Collision Resistance Hash Digest",
				"CORS Security Allowed Origins Headers Methods Preflight Options", "CSRF Protection SameSite Cookies CSRF Tokens Double Submit Cookie",
				"XSS Cross Site Scripting Sanitization Content Security Policy CSP", "SQL Injection Defense Parameterized Queries Prepared Statements ORM",
				"Mutual TLS mTLS Client Certificates Peer Verification Handshake", "API Key Authentication Rate Limiting Header Query Parameter",
				"Zero Trust Security Model Identity Verification Least Privilege Microsegmentation", "JWT JSON Web Token Header Payload Signature Alg Verification",
				"Key Rotation Secret Management HSM KMS HashiCorp Vault", "WAF Web Application Firewall OWASP Top 10 Rule Engine",
				"DDoS Mitigation Anycast IP Rate Limiting SYN Cookies Scrubbing", "Security Headers HSTS X Content Type Options X Frame Options",
				"RBAC Role Based Access Control Permission Matrix Authorization", "ABAC Attribute Based Access Control Policy Engine Dynamic Evaluation",
			},
			Template: "Software Security & Applied Cryptography: %s. Mitigates threat vectors, enforces identity verification, implements encryption at rest/in transit, and protects against OWASP vulnerabilities.",
		},
		{
			Category: "Machine Learning Infrastructure & RAG Engineering",
			Topics: []string{
				"Transformer Architecture Self Attention Multi Head Attention Positional Encoding", "Attention Mechanism Query Key Value Softmax Scoring Context Vector",
				"Vector Embeddings Dense Feature Representations Cosine Similarity Euclidean Distance", "Reciprocal Rank Fusion RRF Sparse BM25 Dense Vector Score Merging",
				"HNSW Hierarchical Navigable Small World Graph Vector Index", "RAG Retrieval Augmented Generation Ingestion Chunking Embeddings Retrieval Prompting",
				"LLM Fine Tuning LoRA Low Rank Adaptation PEFT Parameter Efficient", "Quantization FP16 INT8 INT4 Model Compression Inference Acceleration",
				"Tokenization Byte Pair Encoding BPE Tiktoken WordPiece Subword", "Prompt Engineering System Prompt Chain of Thought Few Shot In Context",
				"Semantic Search BM25 Hybrid Keyword Vector Scoring", "Vector Store Milvus Qdrant Pinecone Weaviate Chroma Indexing",
				"Document Processor HTML Parsing Markdown Conversion Noise Stripping", "Model Context Protocol MCP JSON RPC Stdio Server Agent Integration",
				"Agent Function Calling JSON Schema Tool Registration Execution Loop", "LLM Inference Server vLLM Ollama TensorRT LLM Batching KV Cache",
				"Embedding Generation SentenceTransformers OpenAI Embeddings Feature Vector", "Reranking Cross Encoder BM25 Candidate Rescoring",
				"Text Chunking Fixed Size Overlap Semantic Header Based Chunking", "Evaluations RAGAS Faithfulness Context Precision Answer Relevance",
			},
			Template: "Machine Learning & AI System Infrastructure: %s. Powers retrieval augmented generation, high-dimensional vector search, embedding models, agentic tool calls, and LLM inference pipelines.",
		},
	}

	totalIngested := 0
	startTime := time.Now()

	// Ingest 1,000 distinct SDE documentation topics (10 domains x 20-40 topics x 5 variations)
	for dIdx, domain := range domains {
		if ctx.Err() != nil {
			fmt.Println("⚠️ Ingestion interrupted by signal context.")
			break
		}
		fmt.Printf("📦 Ingesting Domain [%d/10]: %s...\n", dIdx+1, domain.Category)

		for _, topic := range domain.Topics {
			if ctx.Err() != nil {
				break
			}
			for v := 1; v <= 5; v++ {
				if ctx.Err() != nil {
					break
				}
				totalIngested++
				url := fmt.Sprintf("https://sde-knowledge.org/%s/%s/v%d", slugify(domain.Category), slugify(topic), v)
				title, cleanBody := generateArticleMarkdown(domain.Category, topic, v)
				totalTokens := len(strings.Fields(cleanBody))

				// 1. Save to shared Database / Storage
				_ = storage.SaveCrawledDocument(ctx, url, title, cleanBody, totalTokens, "sde_corpus", url)

				// 2. Index into Inverted Index & Autocomplete Trie & Vector Store
				index.GlobalEngine.IndexDocumentDirectly(url, title, cleanBody, totalTokens)
			}
		}
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✅ BATCH INGESTION COMPLETE!\n")
	fmt.Printf("📊 Total SDE Documents Ingested: %d pages\n", totalIngested)
	fmt.Printf("⏱️ Ingestion Time: %v (%.2f docs/sec)\n", duration, float64(totalIngested)/duration.Seconds())

	// Persist index snapshots before exit
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	_ = os.MkdirAll(dataDir, 0755)
	_ = index.GlobalEngine.GetInvertedIndex().SaveSnapshot(filepath.Join(dataDir, "inverted_index.json"))
	_ = index.GlobalEngine.GetVectorIndex().SaveSnapshot(filepath.Join(dataDir, "vector_index.json"))

	// Print Corpus Statistics
	totalDocs, avgLen, vocabSize := index.GlobalEngine.GetInvertedIndex().GetStats()
	fmt.Printf("\n📈 Corpus Metadata & Indexing Statistics:\n")
	fmt.Printf("   - Total Indexed Documents: %d\n", totalDocs)
	fmt.Printf("   - Average Document Length: %.2f tokens\n", avgLen)
	fmt.Printf("   - Vocabulary Size (Unique Terms): %d terms\n", vocabSize)

	// Execute Test Hybrid Search Queries to verify multi-page retrieval
	testQueries := []string{
		"Goroutine GMP Scheduler concurrency",
		"Consistent Hashing Raft consensus CAP",
		"PostgreSQL BTree LSM Tree database indexing",
		"TCP Three Way Handshake TLS encryption",
		"Transformer vector embeddings RAG search",
	}

	fmt.Printf("\n🔍 Testing Hybrid RRF Search Across 1,000+ SDE Corpus Pages:\n")
	for _, q := range testQueries {
		titles, urls, bodies := index.GlobalEngine.GetMetadataMaps()
		bm25Hits := index.GlobalEngine.SearchBM25(q, 10)

		// 2. Dense Vector
		vecHits := index.GlobalEngine.SearchVector(q, 10)

		// 3. RRF Fusion
		fused := search.ReciprocalRankFusion(q, bm25Hits, vecHits, 3, titles, urls, bodies)

		fmt.Printf("\n  Query: '%s' -> Top Hits:\n", q)
		for i, hit := range fused {
			fmt.Printf("    [%d] Title: %s\n        URL: %s (RRF Score: %.6f)\n", i+1, hit.Title, hit.URL, hit.RRFScore)
		}
	}
}

var variationSubtitles = map[int]string{
	1: "Foundational Architecture and Internal Mechanics",
	2: "Production Implementation and State Management",
	3: "Performance Benchmarking and Scale Optimization",
	4: "Fault Tolerance, Reliability, and Resiliency",
	5: "Advanced Distributed Patterns and Case Studies",
}

func generateArticleMarkdown(category, topic string, variation int) (string, string) {
	subtitle := variationSubtitles[variation]
	if subtitle == "" {
		subtitle = fmt.Sprintf("Deep Dive Technical Guide (Part %d)", variation)
	}

	title := fmt.Sprintf("%s: %s — %s", category, topic, subtitle)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))
	sb.WriteString("## Architectural Overview\n\n")
	sb.WriteString(fmt.Sprintf("In high-throughput systems engineering, `%s` represents a critical building block within the `%s` domain. ", topic, category))
	sb.WriteString(fmt.Sprintf("When designing scalable distributed topologies or core systems software, `%s` provides concrete structural guarantees around latency budgets, concurrency hazards, and resource boundaries.\n\n", topic))

	sb.WriteString("## Core Components & Structural Invariants\n\n")
	sb.WriteString(fmt.Sprintf("Implementing `%s` requires rigorous attention to memory alignment, lock contention, cache-line bouncing, and error boundary isolation. ", topic))
	sb.WriteString("Key architectural dimensions include:\n\n")
	sb.WriteString("- **State Synchronization**: Preventing data races and memory corruption across concurrent execution contexts.\n")
	sb.WriteString("- **Data Locality & Cache Efficiency**: Structuring memory layout to minimize L1/L2 cache misses and page faults.\n")
	sb.WriteString("- **Graceful Degradation**: Enforcing bounded backpressure and adaptive load shedding under saturated traffic conditions.\n\n")

	sb.WriteString("## Reference Implementation & Code Example\n\n")
	sb.WriteString(getCodeSnippetForCategory(category, topic))
	sb.WriteString("\n\n")

	sb.WriteString("## Operational Trade-Offs & Complexity Analysis\n\n")
	sb.WriteString(fmt.Sprintf("Production deployment of `%s` involves evaluating time and space complexity against system-level operational overhead:\n\n", topic))
	sb.WriteString("- **Time Complexity**: Optimal amortized bounds ensure predictable tail latency (p99/p99.9).\n")
	sb.WriteString("- **Space Overhead**: Auxiliary heap allocations are controlled through pre-allocated buffers and object pooling.\n")
	sb.WriteString("- **Observability**: Metrics emission (counters, gauges, histograms) and structured tracing enable proactive telemetry monitoring.\n")

	return title, sb.String()
}

func getCodeSnippetForCategory(category, topic string) string {
	switch category {
	case "Data Structures & Algorithms":
		return fmt.Sprintf("```go\n// %s: In-memory structural primitive\ntype StateNode struct {\n\tKey      string\n\tValue    interface{}\n\tChildren []*StateNode\n\tCost     float64\n}\n\nfunc (n *StateNode) Search(target string) (*StateNode, bool) {\n\tif n == nil {\n\t\treturn nil, false\n\t}\n\tif n.Key == target {\n\t\treturn n, true\n\t}\n\tfor _, child := range n.Children {\n\t\tif res, found := child.Search(target); found {\n\t\t\treturn res, true\n\t\t}\n\t}\n\treturn nil, false\n}\n```", topic)
	case "System Design & Distributed Systems":
		return fmt.Sprintf("```go\n// %s: Cluster coordination handler\ntype ClusterManager struct {\n\tmu       sync.RWMutex\n\tnodes    map[string]*NodeState\n\tringHash uint32\n}\n\nfunc (cm *ClusterManager) RoutePayload(ctx context.Context, payload []byte) (string, error) {\n\tcm.mu.RLock()\n\tdefer cm.mu.RUnlock()\n\tif len(cm.nodes) == 0 {\n\t\treturn \"\", errors.New(\"no active replicas available in partition\")\n\t}\n\t// Consistent hashing partition lookup\n\treturn cm.selectTargetNode(payload), nil\n}\n```", topic)
	case "Databases & Storage Engines":
		return fmt.Sprintf("```sql\n-- %s: Storage schema and indexing strategy\nCREATE TABLE storage_partitions (\n    partition_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),\n    resource_key TEXT NOT NULL,\n    payload_body JSONB NOT NULL,\n    version_seq BIGINT NOT NULL DEFAULT 1,\n    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()\n);\n\nCREATE INDEX idx_storage_partitions_key ON storage_partitions (resource_key);\nCREATE INDEX idx_storage_partitions_payload ON storage_partitions USING gin (payload_body);\n```", topic)
	case "Operating Systems & Low-Level Engineering":
		return fmt.Sprintf("```go\n// %s: Low-level syscall and memory descriptor\ntype FileDescriptor struct {\n\tfd     int\n\tevents uint32\n\tbuffer []byte\n}\n\nfunc (d *FileDescriptor) PollEvents(timeout time.Duration) (int, error) {\n\tif d.fd < 0 {\n\t\treturn 0, syscall.EBADF\n\t}\n\t// epoll / kqueue non-blocking event multiplexing\n\treturn syscall.Read(d.fd, d.buffer)\n}\n```", topic)
	case "Networking & Distributed Protocols":
		return fmt.Sprintf("```go\n// %s: Network frame decoder and header parsing\ntype WireHeader struct {\n\tMagicByte  uint8\n\tOpCode     uint8\n\tPayloadLen uint32\n\tStreamID   uint64\n}\n\nfunc ParseWireHeader(r io.Reader) (*WireHeader, error) {\n\tvar hdr WireHeader\n\terr := binary.Read(r, binary.BigEndian, &hdr)\n\treturn &hdr, err\n}\n```", topic)
	case "Go Engineering & Concurrency Systems":
		return fmt.Sprintf("```go\n// %s: Concurrent worker queue with graceful drain\ntype WorkerPool struct {\n\tqueue   chan Job\n\tworkers int\n\twg      sync.WaitGroup\n}\n\nfunc (wp *WorkerPool) Dispatch(ctx context.Context, job Job) error {\n\tselect {\n\tcase <-ctx.Done():\n\t\treturn ctx.Err()\n\tcase wp.queue <- job:\n\t\treturn nil\n\t}\n}\n```", topic)
	case "Cloud Infrastructure, Kubernetes & DevOps":
		return fmt.Sprintf("```yaml\n# %s: Production Deployment & Resource Quotas\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: sde-infrastructure-service\n  labels:\n    app: sde-engine\nspec:\n  replicas: 3\n  template:\n    spec:\n      containers:\n      - name: core-app\n        image: sde-service:v2.1.0\n        resources:\n          limits:\n            cpu: \"2000m\"\n            memory: \"2Gi\"\n          requests:\n            cpu: \"500m\"\n            memory: \"512Mi\"\n```", topic)
	case "Object-Oriented Design & System Patterns":
		return fmt.Sprintf("```go\n// %s: Interface abstraction & Dependency Inversion\ntype PersistenceRepository interface {\n\tGetByID(ctx context.Context, id string) (*DomainEntity, error)\n\tStore(ctx context.Context, entity *DomainEntity) error\n}\n\ntype ServiceHandler struct {\n\trepo PersistenceRepository\n}\n\nfunc NewServiceHandler(repo PersistenceRepository) *ServiceHandler {\n\treturn &ServiceHandler{repo: repo}\n}\n```", topic)
	case "Security, Authentication & Cryptography":
		return fmt.Sprintf("```go\n// %s: Authentication middleware & claims evaluation\ntype Claims struct {\n\tSubject   string   `json:\"sub\"`\n\tAudience  string   `json:\"aud\"`\n\tRoles     []string `json:\"roles\"`\n\tExpiresAt int64    `json:\"exp\"`\n}\n\nfunc (c *Claims) HasRole(targetRole string) bool {\n\tfor _, r := range c.Roles {\n\t\tif r == targetRole {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n```", topic)
	default:
		return fmt.Sprintf("```go\n// %s: Vector space retrieval and ranking\ntype PipelineEngine struct {\n\tDimensions int\n\tThreshold  float64\n}\n\nfunc (pe *PipelineEngine) ComputeRank(queryVec, docVec []float64) float64 {\n\t// Cosine similarity computation\n\tvar dot, normA, normB float64\n\tfor i := 0; i < len(queryVec); i++ {\n\t\tdot += queryVec[i] * docVec[i]\n\t\tnormA += queryVec[i] * queryVec[i]\n\t\tnormB += docVec[i] * docVec[i]\n\t}\n\tif normA == 0 || normB == 0 {\n\t\treturn 0.0\n\t}\n\treturn dot / (math.Sqrt(normA) * math.Sqrt(normB))\n}\n```", topic)
	}
}

func slugify(s string) string {
	var res []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			res = append(res, r)
		} else if r == ' ' || r == '-' || r == '_' || r == '&' {
			res = append(res, '-')
		}
	}
	return string(res)
}
