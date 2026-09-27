package search

import (
	"testing"

	"project-x/pkg/vector/distance"
	"project-x/pkg/vector/hnsw"
)

func TestBM25Search(t *testing.T) {
	bm := NewBM25Index()
	bm.IndexDocument(1, "Distributed LSM-Tree storage engine with Write-Ahead Logging and compaction")
	bm.IndexDocument(2, "Vector database with HNSW graph indexing and SIMD Euclidean distance")
	bm.IndexDocument(3, "Raft consensus protocol with dynamic range splitting and Placement Driver")

	results := bm.Search("LSM-Tree storage", 5)
	if len(results) == 0 {
		t.Fatalf("expected BM25 search results")
	}
	if results[0].DocID != 1 {
		t.Fatalf("expected DocID 1 to be top match, got %d", results[0].DocID)
	}

	resultsRaft := bm.Search("Raft consensus", 5)
	if len(resultsRaft) == 0 || resultsRaft[0].DocID != 3 {
		t.Fatalf("expected DocID 3 for Raft query")
	}
}

func TestHybridSearchFusion(t *testing.T) {
	dim := 4
	bm := NewBM25Index()
	hIndex := hnsw.NewHNSWIndex(dim, distance.L2)

	bm.IndexDocument(1, "PostgreSQL relational database transaction outbox")
	bm.IndexDocument(2, "High throughput vector search with AVX512")

	// Insert vectors
	hIndex.Insert(1, []float32{1.0, 0.0, 0.0, 0.0})
	hIndex.Insert(2, []float32{0.0, 1.0, 0.0, 0.0})

	hybridEngine := NewHybridSearchEngine(bm, hIndex, 0.5)

	// Query matches document 2 in both lexical keywords and vector similarity
	queryVec := []float32{0.0, 0.9, 0.1, 0.0}
	results := hybridEngine.HybridSearch("vector search", queryVec, 5)

	if len(results) == 0 {
		t.Fatalf("expected hybrid search results")
	}
	if results[0].DocID != 2 {
		t.Fatalf("expected DocID 2 to rank highest in hybrid search, got %d", results[0].DocID)
	}
}
