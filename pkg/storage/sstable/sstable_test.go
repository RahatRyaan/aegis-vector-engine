package sstable

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

func TestSSTableBuildAndRead(t *testing.T) {
	tempFile, err := os.CreateTemp("", "sst_test_*.sst")
	if err != nil {
		t.Fatalf("failed to create temp sst file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	tb := NewTableBuilder(tempFile, 1000)
	count := 500

	for i := 0; i < count; i++ {
		k := []byte(fmt.Sprintf("key_%05d", i))
		v := []byte(fmt.Sprintf("val_data_block_%05d", i))
		if err := tb.Add(k, v, uint64(i+1), 1); err != nil {
			t.Fatalf("failed to add to sstable: %v", err)
		}
	}

	if err := tb.Finish(); err != nil {
		t.Fatalf("failed to finish sstable: %v", err)
	}
	_ = tempFile.Close()

	// Reopen for reading
	rf, err := os.Open(tempFile.Name())
	if err != nil {
		t.Fatalf("failed to reopen sst file: %v", err)
	}
	defer rf.Close()

	tr, err := OpenTableReader(rf)
	if err != nil {
		t.Fatalf("failed to open table reader: %v", err)
	}

	for i := 0; i < count; i++ {
		k := []byte(fmt.Sprintf("key_%05d", i))
		expectedVal := []byte(fmt.Sprintf("val_data_block_%05d", i))

		val, seq, op, found, err := tr.Get(k)
		if err != nil {
			t.Fatalf("error getting key %s: %v", string(k), err)
		}
		if !found {
			t.Fatalf("key %s not found in sstable", string(k))
		}
		if !bytes.Equal(val, expectedVal) {
			t.Fatalf("value mismatch for key %s: expected %s, got %s", string(k), string(expectedVal), string(val))
		}
		if seq != uint64(i+1) || op != 1 {
			t.Fatalf("unexpected metadata: seq=%d, op=%d", seq, op)
		}
	}

	// Non-existent key test
	_, _, _, found, err := tr.Get([]byte("non_existent_key_99999"))
	if err != nil {
		t.Fatalf("unexpected error on missing key: %v", err)
	}
	if found {
		t.Fatalf("non-existent key should not be found")
	}
}
