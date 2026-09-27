package memtable

import (
	"fmt"
	"testing"
)

func BenchmarkConcurrentSkiplistSequentialPut(b *testing.B) {
	arena := NewArena(256 * 1024 * 1024)
	sl := NewConcurrentSkiplist(arena)
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key_%010d", i))
		val := []byte("payload_sample_value_128_bytes_data_block_storage_engine_zero_alloc")
		_ = sl.Put(key, val, OpPut, uint64(i+1))
	}
}

func BenchmarkConcurrentSkiplistParallelPut(b *testing.B) {
	arena := NewArena(512 * 1024 * 1024)
	sl := NewConcurrentSkiplist(arena)
	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			key := []byte(fmt.Sprintf("pkey_%010d", i))
			val := []byte("payload_sample_value_128_bytes_data_block_storage_engine_zero_alloc")
			_ = sl.Put(key, val, OpPut, uint64(i))
		}
	})
}
