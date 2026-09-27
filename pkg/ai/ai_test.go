package ai

import (
	"strings"
	"sync"
	"testing"

	"project-x/pkg/vector/distance"
	"project-x/pkg/vector/hnsw"
)

type mockStorageEngine struct {
	mu        sync.RWMutex
	kv        map[string][]byte
	vectorIdx *hnsw.HNSWIndex
}

func newMockStorageEngine(dim int) *mockStorageEngine {
	return &mockStorageEngine{
		kv:        make(map[string][]byte),
		vectorIdx: hnsw.NewHNSWIndex(dim, distance.L2),
	}
}

func (m *mockStorageEngine) Put(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kv[string(key)] = value
	return nil
}

func (m *mockStorageEngine) Get(key []byte) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.kv[string(key)]
	return v, ok, nil
}

func (m *mockStorageEngine) InsertVector(id uint64, vec []float32) {
	m.vectorIdx.Insert(id, vec)
}

func (m *mockStorageEngine) SearchVector(query []float32, k int) []hnsw.DistItem {
	return m.vectorIdx.Search(query, k)
}

func (m *mockStorageEngine) VectorCount() uint64 {
	return m.vectorIdx.Count()
}

func TestAIAgentAndMemoryStore(t *testing.T) {
	dim := 64
	eng := newMockStorageEngine(dim)
	embedder := NewEmbeddingGenerator(dim)
	memStore := NewMemoryStore(eng, embedder)
	agent := NewAIAgent(eng, memStore)

	// 1. Store memories
	_, err := memStore.Store("Project X is a distributed storage engine written in Go and C++", "system_doc", 0.9)
	if err != nil {
		t.Fatalf("failed to store memory 1: %v", err)
	}

	_, err = memStore.Store("The database uses io_uring and AVX-512 SIMD vector kernels", "system_doc", 0.8)
	if err != nil {
		t.Fatalf("failed to store memory 2: %v", err)
	}

	// 2. Test Semantic Recall
	recalled := memStore.Recall("SIMD vector performance", 2)
	if len(recalled) == 0 {
		t.Fatalf("expected recalled memories")
	}

	if !strings.Contains(recalled[0].Item.Content, "SIMD") {
		t.Fatalf("expected top recalled memory to be SIMD memory, got: %s", recalled[0].Item.Content)
	}

	// 3. Test Agent Multi-Turn Chat
	resp, err := agent.Chat("session_1", "What acceleration kernels does Project X use?")
	if err != nil {
		t.Fatalf("agent chat failed: %v", err)
	}

	if len(resp.RecalledMemories) == 0 {
		t.Fatalf("agent should have recalled relevant memories")
	}

	// 4. Test Agent Auto-Store Intent
	respStore, err := agent.Chat("session_1", "remember: The target write throughput is 120,000 IOPS")
	if err != nil {
		t.Fatalf("agent store chat failed: %v", err)
	}
	if len(respStore.ToolExecuted) == 0 {
		t.Fatalf("agent should have executed store_memory tool")
	}
}
