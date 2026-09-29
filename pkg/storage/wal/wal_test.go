package wal

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestWALWriteAndRecover(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "wal_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	w, err := OpenWAL(tempDir, 50*time.Microsecond)
	if err != nil {
		t.Fatalf("failed to open WAL: %v", err)
	}

	count := 1000
	for i := 1; i <= count; i++ {
		k := []byte(fmt.Sprintf("wal_key_%05d", i))
		v := []byte(fmt.Sprintf("wal_val_%05d", i))
		if err := w.Append(uint64(i), RecordTypePut, k, v); err != nil {
			t.Fatalf("failed to append to WAL at index %d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close WAL: %v", err)
	}

	// Reopen and recover
	w2, err := OpenWAL(tempDir, 50*time.Microsecond)
	if err != nil {
		t.Fatalf("failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	recoveredCount := 0
	maxSeq, err := w2.Recover(func(rec *Record) error {
		recoveredCount++
		expectedKey := []byte(fmt.Sprintf("wal_key_%05d", recoveredCount))
		expectedVal := []byte(fmt.Sprintf("wal_val_%05d", recoveredCount))
		if !bytes.Equal(rec.Key, expectedKey) || !bytes.Equal(rec.Value, expectedVal) {
			return fmt.Errorf("mismatch at seq %d", rec.Seq)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("WAL recovery failed: %v", err)
	}
	if recoveredCount != count || maxSeq != uint64(count) {
		t.Fatalf("expected %d recovered records, got %d (maxSeq=%d)", count, recoveredCount, maxSeq)
	}
}

func TestWALConcurrentAppends(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "wal_concurrent_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	w, err := OpenWAL(tempDir, 50*time.Microsecond)
	if err != nil {
		t.Fatalf("failed to open WAL: %v", err)
	}

	workers := 8
	itemsPerWorker := 250
	var wg sync.WaitGroup

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(wid int) {
			defer wg.Done()
			for i := 0; i < itemsPerWorker; i++ {
				seq := uint64(wid*itemsPerWorker + i + 1)
				k := []byte(fmt.Sprintf("worker_%d_key_%d", wid, i))
				v := []byte(fmt.Sprintf("worker_%d_val_%d", wid, i))
				if err := w.Append(seq, RecordTypePut, k, v); err != nil {
					t.Errorf("worker %d failed to append: %v", wid, err)
					return
				}
			}
		}(worker)
	}

	wg.Wait()
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close WAL: %v", err)
	}
}
