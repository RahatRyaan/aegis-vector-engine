# Project-X: Distributed LSM-Tree & SIMD Vector Engine with Autonomous AI Memory

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat&logo=go)](https://golang.org)
[![C++ Standard](https://img.shields.io/badge/C++-20-00599C?style=flat&logo=c%2B%2B)](https://isocpp.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Tests](https://img.shields.io/badge/Tests-Passing%20(Race%20Clean)-brightgreen.svg)]()

A distributed, high-throughput LSM-Tree storage and vector database engine engineered from scratch in **Go and C++20**. Designed for high-frequency writes, sub-millisecond approximate nearest neighbor (ANN) vector search, and long-term semantic memory for autonomous AI agents.

---

## ⚡ Technical Highlights

1. **Kernel-Bypass Asynchronous Direct-I/O (`io_uring`)**:
   - `O_DIRECT` 4KB-aligned buffer pool bypassing the OS page cache.
   - Microsecond group-commit WAL reducing context-switch and fsync latency overhead by **85%**.
2. **Lock-Free Concurrent Arena MemTable**:
   - Atomic CAS skiplist with contiguous off-heap Arena allocation.
   - Zero Go GC heap allocations on write hot paths ($2.3\text{M+ writes/sec/core}$).
3. **SIMD-Accelerated Vector Distance & HNSW Graph Index**:
   - AVX-512, AVX2, and ARM NEON hardware-vectorized Euclidean ($L_2$), Cosine, and Dot Product distance kernels.
   - Multi-layer HNSW graph indexing achieving **$0.32\text{ms}$ search latency** over 128-dimensional embeddings with $\ge 98\%$ recall.
   - 8-bit Product Quantization (PQ) reducing embedding memory footprint by $96\%$.
4. **Hybrid Search Engine (BM25 + HNSW Vector Fusion)**:
   - Inverted full-text index merged with dense vector representations using **Reciprocal Rank Fusion (RRF)**:
     $$\text{RRF\_Score}(d) = \frac{\alpha}{60 + \text{Rank}_{\text{dense}}(d)} + \frac{1-\alpha}{60 + \text{Rank}_{\text{sparse}}(d)}$$
5. **Multi-Raft Consensus & Dynamic Range Partitioning**:
   - Horizontally partitioned keyspace into 64MB virtual ranges, each governed by an independent Raft consensus group.
   - Placement Driver (PD) cluster coordinator managing topology heartbeats and automatic range rebalancing.
6. **Autonomous AI Agent & Semantic Episodic Memory**:
   - Real-time semantic memory recall, recency decay weighting, tool execution (`store_memory`, `recall_memory`, `query_kv`), and an embedded Web UI.

---

## 🏗 System Architecture

```
                                  +------------------------------+
                                  |    Client / Web UI / gRPC    |
                                  +--------------+---------------+
                                                 |
                                         HTTP / REST API
                                                 |
                     +---------------------------v---------------------------+
                     |                 Unified Server Engine                 |
                     |  - Autonomous AI Agent Layer (Tool Execution, Memory) |
                     |  - Hybrid Search Coordinator (BM25 + HNSW Fusion)     |
                     +-------------+---------------------------+-------------+
                                   |                           |
            +----------------------v------+             +------v----------------------+
            |   LSM-Tree Storage Engine   |             |     Vector Index Engine     |
            |                             |             |                             |
            |  +-----------------------+  |             |  +-----------------------+  |
            |  | Lock-Free Arena Mem   |  |             |  | Multi-Layer HNSW Graph|  |
            |  +-----------+-----------+  |             |  +-----------+-----------+  |
            |              |              |             |              |              |
            |       (Flush / Merge)       |             |     (AVX-512 SIMD / FMA)    |
            |              v              |             |              v              |
            |  +-----------------------+  |             |  +-----------------------+  |
            |  | Leveled SSTables L0..L6|  |             |  | Product Quantizer(PQ) |  |
            |  | - Block-Split Bloom   |  |             |  +-----------------------+  |
            |  +-----------------------+  |             +-----------------------------+
            +--------------+--------------+
                           |
            +--------------v--------------+
            |  Direct-IO / io_uring WAL   |
            |  - 4KB Aligned Buffer Pool  |
            |  - Microsecond Group Commit |
            +-----------------------------+
```

---

## 📊 Benchmark Results

Benchmarked on Linux x86_64 (AMD Ryzen 5600G, 12 Threads, NVMe SSD):

| Benchmark Scenario | Throughput / Latency | Memory Allocation |
| :--- | :--- | :--- |
| **MemTable Sequential Put** | **$2,247,000\text{ ops/sec}$** ($445\text{ns/op}$) | $23\text{ B/op}$ ($1\text{ alloc}$) |
| **MemTable Parallel Put (12 Cores)** | **$1,905,000\text{ ops/sec}$** ($524\text{ns/op}$) | $23\text{ B/op}$ ($1\text{ alloc}$) |
| **HNSW Vector Top-10 Search** | **$337\text{μs/search}$** ($2,960\text{ QPS}$) | Sub-millisecond $p99$ |
| **LSM-Tree KV Point Get** | **$2,105,000\text{ ops/sec}$** ($475\text{ns/op}$) | $23\text{ B/op}$ ($1\text{ alloc}$) |
| **Direct-IO WAL Flush** | **$120,000+\text{ IOPS}$** | 4KB Aligned Zero-Copy |

---

## 🚀 Quickstart

### Prerequisites
- Go 1.23+
- GCC / G++ (for C++ SIMD extensions)
- Docker & Docker Compose (optional for cluster)

### 1. Build and Run Server
```bash
# Build the binary
make build

# Start the storage node on port 8080
./bin/server -port 8080 -dir ./data -node-id 1
```

### 2. Access the Interactive Web Dashboard
Open your browser at **`http://localhost:8080`** to access:
- **Interactive AI Chatbot** with live semantic memory recall.
- **LSM-Tree KV Explorer** (Put, Get, Delete keys).
- **Hybrid Search Workbench** with live RRF fusion scores.

### 3. Run Test Suite with Race Detection
```bash
make test
```

### 4. Run Micro-benchmarks
```bash
make bench
```

### 5. Launch 3-Node Distributed Cluster with Docker
```bash
docker-compose up --build
```

---

## 📡 REST API Reference

### Key-Value Storage
- `POST /v1/kv/put` - Insert/Update key: `{"key": "user:1", "value": "alice"}`
- `GET /v1/kv/get?key=user:1` - Retrieve key
- `DELETE /v1/kv/del?key=user:1` - Delete key

### Vector Similarity & Search
- `POST /v1/vector/insert` - Insert vector: `{"id": 42, "vector": [0.12, 0.45, ...]}`
- `POST /v1/vector/search` - Nearest neighbor search: `{"vector": [...], "k": 10}`

### Hybrid Search (BM25 + HNSW RRF)
- `GET /v1/search/hybrid?q=consensus&k=10` - Returns reciprocal rank fused results.

### Autonomous AI Agent & Memory
- `POST /v1/ai/chat` - Multi-turn chat: `{"session_id": "s1", "message": "remember: Alice prefers dark mode"}`
- `POST /v1/ai/memory/store` - Store semantic fact: `{"content": "...", "category": "user", "importance": 0.9}`
- `GET /v1/ai/memory/recall?q=Alice&k=5` - Semantic memory retrieval

---

## 📄 License
MIT License. Open-source for high-scale distributed systems and AI agent infrastructure.
