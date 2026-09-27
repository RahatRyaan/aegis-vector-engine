package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"project-x/pkg/search"
	"project-x/pkg/vector/hnsw"
)

type StorageEngine interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, bool, error)
	InsertVector(id uint64, vec []float32)
	SearchVector(query []float32, k int) []hnsw.DistItem
	VectorCount() uint64
}

type MemoryItem struct {
	ID         uint64            `json:"id"`
	Content    string            `json:"content"`
	Category   string            `json:"category"`
	Timestamp  time.Time         `json:"timestamp"`
	Importance float32           `json:"importance"` // 0.0 to 1.0
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type MemoryStore struct {
	engine       StorageEngine
	embedder     *EmbeddingGenerator
	hybrid       *search.HybridSearchEngine
	bm25         *search.BM25Index
	nextMemoryID uint64
	memories     map[uint64]*MemoryItem
	mu           sync.RWMutex
}

func NewMemoryStore(engine StorageEngine, embedder *EmbeddingGenerator) *MemoryStore {
	bm := search.NewBM25Index()
	hybrid := search.NewHybridSearchEngine(bm, nil, 0.6)

	return &MemoryStore{
		engine:       engine,
		embedder:     embedder,
		bm25:         bm,
		hybrid:       hybrid,
		memories:     make(map[uint64]*MemoryItem),
		nextMemoryID: 1,
	}
}

func (ms *MemoryStore) Store(content, category string, importance float32) (*MemoryItem, error) {
	if importance <= 0 {
		importance = 0.5
	}
	if importance > 1.0 {
		importance = 1.0
	}

	id := atomic.AddUint64(&ms.nextMemoryID, 1)
	item := &MemoryItem{
		ID:         id,
		Content:    content,
		Category:   category,
		Timestamp:  time.Now(),
		Importance: importance,
		Metadata:   make(map[string]string),
	}

	// 1. Generate dense vector
	vec := ms.embedder.GenerateEmbedding(content)

	// 2. Persist to storage engine LSM-tree KV store
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	kvKey := []byte(fmt.Sprintf("mem:%010d", id))
	if err := ms.engine.Put(kvKey, encoded); err != nil {
		return nil, err
	}

	// 3. Index vector in HNSW
	ms.engine.InsertVector(id, vec)

	// 4. Index in lexical BM25
	ms.bm25.IndexDocument(id, content)

	ms.mu.Lock()
	ms.memories[id] = item
	ms.mu.Unlock()

	return item, nil
}

type ScoredMemory struct {
	Item       *MemoryItem `json:"item"`
	Score      float32     `json:"score"`
	Similarity float32     `json:"similarity"`
	Recency    float32     `json:"recency"`
}

func (ms *MemoryStore) Recall(query string, topK int) []ScoredMemory {
	if topK <= 0 {
		topK = 5
	}

	queryVec := ms.embedder.GenerateEmbedding(query)
	hnswResults := ms.engine.SearchVector(queryVec, topK*3)
	bm25Results := ms.bm25.Search(query, topK*3)

	candidateIDs := make(map[uint64]float32) // ID -> dense similarity score
	for _, it := range hnswResults {
		// Invert L2 distance to similarity metric in [0, 1]
		sim := float32(1.0 / (1.0 + it.Dist))
		candidateIDs[it.ID] = sim
	}

	for _, it := range bm25Results {
		if _, exists := candidateIDs[it.DocID]; !exists {
			candidateIDs[it.DocID] = 0.3 // baseline score for lexical hits
		}
	}

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var scoredList []ScoredMemory
	now := time.Now()

	for id, sim := range candidateIDs {
		item, exists := ms.memories[id]
		if !exists {
			continue
		}

		// Calculate recency score: exponential decay over hours
		hoursElapsed := float32(now.Sub(item.Timestamp).Hours())
		recency := float32(math.Exp(-float64(hoursElapsed / 72.0))) // 72-hour half-life

		// Composite score: 0.6 * Similarity + 0.2 * Recency + 0.2 * Importance
		totalScore := 0.6*sim + 0.2*recency + 0.2*item.Importance

		scoredList = append(scoredList, ScoredMemory{
			Item:       item,
			Score:      totalScore,
			Similarity: sim,
			Recency:    recency,
		})
	}

	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].Score > scoredList[j].Score
	})

	if len(scoredList) > topK {
		scoredList = scoredList[:topK]
	}

	return scoredList
}

func (ms *MemoryStore) Count() int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return len(ms.memories)
}
