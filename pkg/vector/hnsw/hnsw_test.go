package hnsw

import (
	"math/rand"
	"testing"
	"project-x/pkg/vector/distance"
)

func generateRandomVector(dim int) []float32 {
	v := make([]float32, dim)
	for i := 0; i < dim; i++ {
		v[i] = rand.Float32()
	}
	return v
}

func TestHNSWBasicInsertAndSearch(t *testing.T) {
	dim := 64
	idx := NewHNSWIndex(dim, distance.L2)

	numVectors := 1000
	vectors := make([][]float32, numVectors)

	for i := 0; i < numVectors; i++ {
		vectors[i] = generateRandomVector(dim)
		idx.Insert(uint64(i+1), vectors[i])
	}

	if idx.Count() != uint64(numVectors) {
		t.Fatalf("expected count %d, got %d", numVectors, idx.Count())
	}

	// Search exact matches (query = vector[42])
	query := vectors[42]
	results := idx.Search(query, 5)

	if len(results) == 0 {
		t.Fatalf("no results returned from HNSW search")
	}

	// Top 1 result must be vector 42 with distance 0.0
	if results[0].ID != 43 { // ID is index + 1
		t.Fatalf("expected top result to be ID 43, got ID %d with dist %f", results[0].ID, results[0].Dist)
	}
	if results[0].Dist > 1e-4 {
		t.Fatalf("expected near-zero distance for exact vector, got %f", results[0].Dist)
	}
}

func BenchmarkHNSWSearch(b *testing.B) {
	dim := 128
	idx := NewHNSWIndex(dim, distance.L2)
	numVectors := 5000

	for i := 0; i < numVectors; i++ {
		v := generateRandomVector(dim)
		idx.Insert(uint64(i+1), v)
	}

	query := generateRandomVector(dim)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = idx.Search(query, 10)
	}
}
