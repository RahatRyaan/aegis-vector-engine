package benchmarks

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"project-x/pkg/server"
	"project-x/pkg/vector/distance"
)

func TestEngineEndToEnd(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "eng_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := server.Config{
		DataDir:        tempDir,
		NodeID:         1,
		VectorDim:      64,
		VectorMetric:   distance.L2,
		MemTableSize:   16 * 1024 * 1024,
		WALBatchWindow: 50 * time.Microsecond,
	}

	eng, err := server.OpenEngine(cfg)
	if err != nil {
		t.Fatalf("failed to open engine: %v", err)
	}
	defer eng.Close()

	// 1. Test KV operations
	numKV := 1000
	for i := 0; i < numKV; i++ {
		k := []byte(fmt.Sprintf("user_key_%05d", i))
		v := []byte(fmt.Sprintf("user_data_payload_%05d", i))
		if err := eng.Put(k, v); err != nil {
			t.Fatalf("failed to put key %d: %v", i, err)
		}
	}

	for i := 0; i < numKV; i++ {
		k := []byte(fmt.Sprintf("user_key_%05d", i))
		expectedVal := []byte(fmt.Sprintf("user_data_payload_%05d", i))
		val, found, err := eng.Get(k)
		if err != nil || !found {
			t.Fatalf("failed to get key %s: found=%v, err=%v", string(k), found, err)
		}
		if string(val) != string(expectedVal) {
			t.Fatalf("value mismatch: expected %s, got %s", string(expectedVal), string(val))
		}
	}

	// 2. Test Vector operations
	numVectors := 500
	for i := 1; i <= numVectors; i++ {
		vec := make([]float32, 64)
		for d := 0; d < 64; d++ {
			vec[d] = rand.Float32()
		}
		eng.InsertVector(uint64(i), vec)
	}

	query := make([]float32, 64)
	for d := 0; d < 64; d++ {
		query[d] = rand.Float32()
	}

	results := eng.SearchVector(query, 10)
	if len(results) == 0 {
		t.Fatalf("expected non-empty vector search results")
	}
}

func BenchmarkEngineKVPut(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "eng_bench_put_*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := server.Config{
		DataDir:        tempDir,
		NodeID:         1,
		VectorDim:      64,
		VectorMetric:   distance.L2,
		MemTableSize:   64 * 1024 * 1024,
		WALBatchWindow: 50 * time.Microsecond,
	}

	eng, err := server.OpenEngine(cfg)
	if err != nil {
		b.Fatalf("failed to open engine: %v", err)
	}
	defer eng.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		k := []byte(fmt.Sprintf("k_%08d", i))
		v := []byte("payload_128_bytes_test_data_directio_group_commit_engine")
		_ = eng.Put(k, v)
	}
}

func BenchmarkEngineKVGet(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "eng_bench_get_*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := server.Config{
		DataDir:        tempDir,
		NodeID:         1,
		VectorDim:      64,
		VectorMetric:   distance.L2,
		MemTableSize:   64 * 1024 * 1024,
		WALBatchWindow: 50 * time.Microsecond,
	}

	eng, err := server.OpenEngine(cfg)
	if err != nil {
		b.Fatalf("failed to open engine: %v", err)
	}
	defer eng.Close()

	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		k := []byte(fmt.Sprintf("k_%08d", i))
		v := []byte("payload_128_bytes_test_data_directio_group_commit_engine")
		_ = eng.Put(k, v)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		k := []byte(fmt.Sprintf("k_%08d", i%numKeys))
		_, _, _ = eng.Get(k)
	}
}
