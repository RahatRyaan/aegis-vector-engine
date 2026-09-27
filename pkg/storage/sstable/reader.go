package sstable

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
)

var (
	ErrInvalidSSTable = errors.New("sstable: invalid magic number or footer")
	ErrKeyNotFound    = errors.New("sstable: key not found")
)

type Footer struct {
	FilterHandle BlockHandle
	IndexHandle  BlockHandle
	NumEntries   uint64
	Magic        uint64
}

type TableReader struct {
	file        *os.File
	footer      Footer
	bloomFilter *BloomFilter
	indexBlock  []BlockEntry
	fileSize    int64
}

func OpenTableReader(file *os.File) (*TableReader, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()
	if fileSize < FooterSize {
		return nil, ErrInvalidSSTable
	}

	// 1. Read Footer
	var footerBuf [FooterSize]byte
	if _, err := file.ReadAt(footerBuf[:], fileSize-FooterSize); err != nil {
		return nil, err
	}

	footer := Footer{
		FilterHandle: BlockHandle{
			Offset: binary.LittleEndian.Uint64(footerBuf[0:8]),
			Size:   binary.LittleEndian.Uint64(footerBuf[8:16]),
		},
		IndexHandle: BlockHandle{
			Offset: binary.LittleEndian.Uint64(footerBuf[16:24]),
			Size:   binary.LittleEndian.Uint64(footerBuf[24:32]),
		},
		NumEntries: binary.LittleEndian.Uint64(footerBuf[32:40]),
		Magic:      binary.LittleEndian.Uint64(footerBuf[40:48]),
	}

	if footer.Magic != SSTableMagicNumber {
		return nil, ErrInvalidSSTable
	}

	tr := &TableReader{
		file:     file,
		footer:   footer,
		fileSize: fileSize,
	}

	// 2. Load Bloom Filter Block
	if footer.FilterHandle.Size > 0 {
		filterData := make([]byte, footer.FilterHandle.Size)
		if _, err := file.ReadAt(filterData, int64(footer.FilterHandle.Offset)); err != nil {
			return nil, err
		}
		tr.bloomFilter = LoadBloomFilter(filterData)
	}

	// 3. Load Index Block
	if footer.IndexHandle.Size > 0 {
		indexData := make([]byte, footer.IndexHandle.Size)
		if _, err := file.ReadAt(indexData, int64(footer.IndexHandle.Offset)); err != nil {
			return nil, err
		}
		br, err := NewBlockReader(indexData)
		if err != nil {
			return nil, err
		}
		tr.indexBlock, err = br.Entries()
		if err != nil {
			return nil, err
		}
	}

	return tr, nil
}

func (tr *TableReader) MayContain(key []byte) bool {
	if tr.bloomFilter == nil {
		return true
	}
	return tr.bloomFilter.MayContain(key)
}

func (tr *TableReader) Get(key []byte) ([]byte, uint64, byte, bool, error) {
	if !tr.MayContain(key) {
		return nil, 0, 0, false, nil
	}

	// Binary search in index block to locate the data block
	blockHandle, found := tr.findIndexBlock(key)
	if !found {
		return nil, 0, 0, false, nil
	}

	data := make([]byte, blockHandle.Size)
	if _, err := tr.file.ReadAt(data, int64(blockHandle.Offset)); err != nil {
		return nil, 0, 0, false, err
	}

	br, err := NewBlockReader(data)
	if err != nil {
		return nil, 0, 0, false, err
	}

	entries, err := br.Entries()
	if err != nil {
		return nil, 0, 0, false, err
	}

	for _, entry := range entries {
		if bytes.Equal(entry.Key, key) {
			return entry.Value, entry.Seq, entry.Op, true, nil
		}
	}

	return nil, 0, 0, false, nil
}

func (tr *TableReader) findIndexBlock(key []byte) (BlockHandle, bool) {
	for _, idx := range tr.indexBlock {
		if bytes.Compare(key, idx.Key) <= 0 {
			if len(idx.Value) < 16 {
				return BlockHandle{}, false
			}
			return BlockHandle{
				Offset: binary.LittleEndian.Uint64(idx.Value[0:8]),
				Size:   binary.LittleEndian.Uint64(idx.Value[8:16]),
			}, true
		}
	}
	return BlockHandle{}, false
}

func (tr *TableReader) AllEntries() ([]BlockEntry, error) {
	var all []BlockEntry
	for _, idx := range tr.indexBlock {
		if len(idx.Value) < 16 {
			continue
		}
		offset := binary.LittleEndian.Uint64(idx.Value[0:8])
		size := binary.LittleEndian.Uint64(idx.Value[8:16])

		data := make([]byte, size)
		if _, err := tr.file.ReadAt(data, int64(offset)); err != nil {
			return nil, err
		}
		br, err := NewBlockReader(data)
		if err != nil {
			return nil, err
		}
		entries, err := br.Entries()
		if err != nil {
			return nil, err
		}
		all = append(all, entries...)
	}
	return all, nil
}

func (tr *TableReader) Close() error {
	return tr.file.Close()
}
