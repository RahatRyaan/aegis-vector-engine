package memtable

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

func TestArenaAllocation(t *testing.T) {
	arena := NewArena(1024 * 1024)
	data := []byte("hello world high throughput storage")
	off, err := arena.PutBytes(data)
	if err != nil {
		t.Fatalf("unexpected arena error: %v", err)
	}

	retrieved := arena.GetBytes(off, uint32(len(data)))
	if !bytes.Equal(data, retrieved) {
		t.Fatalf("expected %s, got %s", string(data), string(retrieved))
	}
}

func TestConcurrentSkiplistBasic(t *testing.T) {
	arena := NewArena(16 * 1024 * 1024)
	sl := NewConcurrentSkiplist(arena)

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key_%05d", i))
		val := []byte(fmt.Sprintf("val_%05d", i))
		if err := sl.Put(key, val, OpPut, uint64(i+1)); err != nil {
			t.Fatalf("failed to put key %d: %v", i, err)
		}
	}

	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key_%05d", i))
		valExpected := []byte(fmt.Sprintf("val_%05d", i))
		val, seq, op, found := sl.Get(key)
		if !found {
			t.Fatalf("key %s not found", string(key))
		}
		if !bytes.Equal(val, valExpected) {
			t.Fatalf("expected val %s, got %s", string(valExpected), string(val))
		}
		if seq != uint64(i+1) || op != OpPut {
			t.Fatalf("unexpected seq or op: seq=%d, op=%d", seq, op)
		}
	}

	// Test Iterator
	it := sl.NewIterator()
	it.SeekToFirst()
	count := 0
	var prevKey []byte
	for it.Valid() {
		k := it.Key()
		if prevKey != nil && bytes.Compare(prevKey, k) >= 0 {
			t.Fatalf("iterator out of order: prev=%s, curr=%s", string(prevKey), string(k))
		}
		prevKey = append([]byte(nil), k...)
		count++
		it.Next()
	}

	if count != 1000 {
		t.Fatalf("expected 1000 items in iterator, got %d", count)
	}
}

func TestConcurrentSkiplistMultiThreaded(t *testing.T) {
	arena := NewArena(64 * 1024 * 1024)
	sl := NewConcurrentSkiplist(arena)

	var wg sync.WaitGroup
	workers := 16
	itemsPerWorker := 500

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				key := []byte(fmt.Sprintf("worker_%02d_key_%04d", workerID, i))
				val := []byte(fmt.Sprintf("worker_%02d_val_%04d", workerID, i))
				_ = sl.Put(key, val, OpPut, uint64(workerID*itemsPerWorker+i))
			}
		}(w)
	}

	wg.Wait()

	if sl.Count() != uint64(workers*itemsPerWorker) {
		t.Fatalf("expected %d elements, got %d", workers*itemsPerWorker, sl.Count())
	}
}
