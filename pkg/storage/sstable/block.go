package sstable

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	DefaultBlockSize       = 4096 // 4KB
	DefaultRestartInterval = 16
)

var (
	ErrBlockCorrupt = errors.New("sstable: corrupted block")
)

type BlockBuilder struct {
	buf             bytes.Buffer
	restarts        []uint32
	restartInterval int
	counter         int
	lastKey         []byte
	finished        bool
}

func NewBlockBuilder(restartInterval int) *BlockBuilder {
	if restartInterval <= 0 {
		restartInterval = DefaultRestartInterval
	}
	bb := &BlockBuilder{
		restartInterval: restartInterval,
		restarts:        []uint32{0}, // First restart point at offset 0
	}
	return bb
}

func (bb *BlockBuilder) Add(key, value []byte, seq uint64, op byte) {
	if bb.finished {
		return
	}

	shared := 0
	if bb.counter < bb.restartInterval {
		minLen := len(bb.lastKey)
		if len(key) < minLen {
			minLen = len(key)
		}
		for shared < minLen && bb.lastKey[shared] == key[shared] {
			shared++
		}
	} else {
		// New restart point
		bb.restarts = append(bb.restarts, uint32(bb.buf.Len()))
		bb.counter = 0
	}

	nonShared := len(key) - shared
	var varintBuf [binary.MaxVarintLen64]byte

	// Write shared key len, non-shared key len, val len
	n := binary.PutUvarint(varintBuf[:], uint64(shared))
	bb.buf.Write(varintBuf[:n])

	n = binary.PutUvarint(varintBuf[:], uint64(nonShared))
	bb.buf.Write(varintBuf[:n])

	n = binary.PutUvarint(varintBuf[:], uint64(len(value)))
	bb.buf.Write(varintBuf[:n])

	// Key delta & value
	bb.buf.Write(key[shared:])
	bb.buf.Write(value)

	// Seq (8 bytes) & Op (1 byte)
	var meta [9]byte
	binary.LittleEndian.PutUint64(meta[0:8], seq)
	meta[8] = op
	bb.buf.Write(meta[:])

	bb.lastKey = append(bb.lastKey[:0], key...)
	bb.counter++
}

func (bb *BlockBuilder) Finish() []byte {
	if bb.finished {
		return bb.buf.Bytes()
	}

	// Write restarts array
	for _, r := range bb.restarts {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], r)
		bb.buf.Write(b[:])
	}

	// Write number of restarts
	var numRestarts [4]byte
	binary.LittleEndian.PutUint32(numRestarts[:], uint32(len(bb.restarts)))
	bb.buf.Write(numRestarts[:])

	bb.finished = true
	return bb.buf.Bytes()
}

func (bb *BlockBuilder) CurrentSize() int {
	return bb.buf.Len() + len(bb.restarts)*4 + 4
}

func (bb *BlockBuilder) Reset() {
	bb.buf.Reset()
	bb.restarts = bb.restarts[:0]
	bb.restarts = append(bb.restarts, 0)
	bb.counter = 0
	bb.lastKey = bb.lastKey[:0]
	bb.finished = false
}

// BlockReader reads an immutable encoded block
type BlockEntry struct {
	Key   []byte
	Value []byte
	Seq   uint64
	Op    byte
}

type BlockReader struct {
	data        []byte
	numRestarts uint32
	restartOffset uint32
}

func NewBlockReader(data []byte) (*BlockReader, error) {
	if len(data) < 4 {
		return nil, ErrBlockCorrupt
	}
	numRestarts := binary.LittleEndian.Uint32(data[len(data)-4:])
	restartOffset := uint32(len(data)) - 4 - (numRestarts * 4)
	if restartOffset > uint32(len(data)) {
		return nil, ErrBlockCorrupt
	}

	return &BlockReader{
		data:          data,
		numRestarts:   numRestarts,
		restartOffset: restartOffset,
	}, nil
}

func (br *BlockReader) Entries() ([]BlockEntry, error) {
	var entries []BlockEntry
	offset := uint32(0)
	var lastKey []byte

	for offset < br.restartOffset {
		shared, n := binary.Uvarint(br.data[offset:])
		if n <= 0 {
			return nil, ErrBlockCorrupt
		}
		offset += uint32(n)

		nonShared, n := binary.Uvarint(br.data[offset:])
		if n <= 0 {
			return nil, ErrBlockCorrupt
		}
		offset += uint32(n)

		valLen, n := binary.Uvarint(br.data[offset:])
		if n <= 0 {
			return nil, ErrBlockCorrupt
		}
		offset += uint32(n)

		keyDelta := br.data[offset : offset+uint32(nonShared)]
		offset += uint32(nonShared)

		val := br.data[offset : offset+uint32(valLen)]
		offset += uint32(valLen)

		seq := binary.LittleEndian.Uint64(br.data[offset : offset+8])
		op := br.data[offset+8]
		offset += 9

		fullKey := make([]byte, int(shared)+len(keyDelta))
		copy(fullKey, lastKey[:shared])
		copy(fullKey[shared:], keyDelta)

		valueCopy := make([]byte, len(val))
		copy(valueCopy, val)

		entries = append(entries, BlockEntry{
			Key:   fullKey,
			Value: valueCopy,
			Seq:   seq,
			Op:    op,
		})

		lastKey = fullKey
	}

	return entries, nil
}
