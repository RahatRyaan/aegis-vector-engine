package sstable

import (
	"encoding/binary"
	"os"
)

const (
	SSTableMagicNumber = uint64(0xdb476d1c94d049bb)
	FooterSize         = 48
)

type BlockHandle struct {
	Offset uint64
	Size   uint64
}

type TableBuilder struct {
	file         *os.File
	dataBlock    *BlockBuilder
	indexBlock   *BlockBuilder
	bloomFilter  *BloomFilter
	offset       uint64
	numEntries   uint64
	smallestKey  []byte
	largestKey   []byte
	lastKeyAdded []byte
}

func NewTableBuilder(file *os.File, expectedKeys int) *TableBuilder {
	return &TableBuilder{
		file:        file,
		dataBlock:   NewBlockBuilder(DefaultRestartInterval),
		indexBlock:  NewBlockBuilder(1),
		bloomFilter: NewBloomFilter(expectedKeys, 10),
		offset:      0,
	}
}

func (tb *TableBuilder) Add(key, value []byte, seq uint64, op byte) error {
	if len(tb.smallestKey) == 0 {
		tb.smallestKey = append([]byte(nil), key...)
	}
	tb.largestKey = append(tb.largestKey[:0], key...)
	tb.lastKeyAdded = append(tb.lastKeyAdded[:0], key...)

	tb.bloomFilter.Add(key)
	tb.dataBlock.Add(key, value, seq, op)
	tb.numEntries++

	if tb.dataBlock.CurrentSize() >= DefaultBlockSize {
		if err := tb.flushDataBlock(); err != nil {
			return err
		}
	}
	return nil
}

func (tb *TableBuilder) flushDataBlock() error {
	if tb.dataBlock.buf.Len() == 0 {
		return nil
	}

	blockBytes := tb.dataBlock.Finish()
	blockSize := uint64(len(blockBytes))
	blockOffset := tb.offset

	n, err := tb.file.Write(blockBytes)
	if err != nil {
		return err
	}
	tb.offset += uint64(n)

	// Add index entry: key = lastKeyAdded, value = encoded BlockHandle
	var handleBuf [16]byte
	binary.LittleEndian.PutUint64(handleBuf[0:8], blockOffset)
	binary.LittleEndian.PutUint64(handleBuf[8:16], blockSize)
	tb.indexBlock.Add(tb.lastKeyAdded, handleBuf[:], 0, 0)

	tb.dataBlock.Reset()
	return nil
}

func (tb *TableBuilder) Finish() error {
	// Flush remaining data in data block
	if err := tb.flushDataBlock(); err != nil {
		return err
	}

	// 1. Write Bloom Filter Block
	filterBytes := tb.bloomFilter.Bytes()
	filterHandle := BlockHandle{
		Offset: tb.offset,
		Size:   uint64(len(filterBytes)),
	}
	n, err := tb.file.Write(filterBytes)
	if err != nil {
		return err
	}
	tb.offset += uint64(n)

	// 2. Write Index Block
	indexBytes := tb.indexBlock.Finish()
	indexHandle := BlockHandle{
		Offset: tb.offset,
		Size:   uint64(len(indexBytes)),
	}
	n, err = tb.file.Write(indexBytes)
	if err != nil {
		return err
	}
	tb.offset += uint64(n)

	// 3. Write Footer (48 bytes)
	var footer [FooterSize]byte
	binary.LittleEndian.PutUint64(footer[0:8], filterHandle.Offset)
	binary.LittleEndian.PutUint64(footer[8:16], filterHandle.Size)
	binary.LittleEndian.PutUint64(footer[16:24], indexHandle.Offset)
	binary.LittleEndian.PutUint64(footer[24:32], indexHandle.Size)
	binary.LittleEndian.PutUint64(footer[32:40], tb.numEntries)
	binary.LittleEndian.PutUint64(footer[40:48], SSTableMagicNumber)

	_, err = tb.file.Write(footer[:])
	if err != nil {
		return err
	}

	return tb.file.Sync()
}

func (tb *TableBuilder) SmallestKey() []byte {
	return tb.smallestKey
}

func (tb *TableBuilder) LargestKey() []byte {
	return tb.largestKey
}

func (tb *TableBuilder) NumEntries() uint64 {
	return tb.numEntries
}
