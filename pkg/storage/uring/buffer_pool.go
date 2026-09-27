package uring

import (
	"sync"
	"unsafe"
)

const (
	BlockAlignment = 4096 // 4KB page alignment required for O_DIRECT
	DefaultBlockSize = 64 * 1024 // 64KB direct IO block
)

type AlignedBufferPool struct {
	blockSize int
	pool      sync.Pool
}

func NewAlignedBufferPool(blockSize int) *AlignedBufferPool {
	if blockSize <= 0 {
		blockSize = DefaultBlockSize
	}
	// Ensure block size is a multiple of 4KB
	if rem := blockSize % BlockAlignment; rem != 0 {
		blockSize += BlockAlignment - rem
	}

	p := &AlignedBufferPool{
		blockSize: blockSize,
	}

	p.pool = sync.Pool{
		New: func() interface{} {
			return AllocAligned(p.blockSize)
		},
	}

	return p
}

func AllocAligned(size int) []byte {
	// Allocate size + alignment to ensure we can slice an aligned chunk
	raw := make([]byte, size+BlockAlignment)
	addr := uintptr(unsafe.Pointer(&raw[0]))
	offset := 0
	if rem := int(addr % uintptr(BlockAlignment)); rem != 0 {
		offset = BlockAlignment - rem
	}
	return raw[offset : offset+size]
}

func (p *AlignedBufferPool) Get() []byte {
	return p.pool.Get().([]byte)
}

func (p *AlignedBufferPool) Put(buf []byte) {
	if cap(buf) < p.blockSize {
		return
	}
	// Zero out slice before returning to pool
	for i := range buf {
		buf[i] = 0
	}
	p.pool.Put(buf[:p.blockSize])
}

func IsAligned(buf []byte) bool {
	if len(buf) == 0 {
		return true
	}
	return uintptr(unsafe.Pointer(&buf[0]))%uintptr(BlockAlignment) == 0
}
