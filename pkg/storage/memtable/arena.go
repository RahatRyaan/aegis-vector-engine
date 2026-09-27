package memtable

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

const (
	MaxArenaSize = 1 << 30 // 1GB max per memtable arena
	nodeAlign    = int(unsafe.Sizeof(uintptr(0)))
)

var (
	ErrArenaFull = errors.New("arena: memory limit reached")
)

type Arena struct {
	buf    []byte
	offset uint64
	cap    uint64
}

func NewArena(capacity uint64) *Arena {
	if capacity == 0 {
		capacity = 64 << 20 // 64 MB default
	}
	return &Arena{
		buf:    make([]byte, capacity),
		offset: 1, // Offset 0 is reserved as null offset
		cap:    capacity,
	}
}

func (a *Arena) Alloc(size uint32) (uint32, error) {
	pad := uint32(0)
	curr := atomic.LoadUint64(&a.offset)

	for {
		rem := (curr + uint64(pad)) % uint64(nodeAlign)
		if rem != 0 {
			pad = uint32(uint64(nodeAlign) - rem)
		} else {
			pad = 0
		}

		newOffset := curr + uint64(size) + uint64(pad)
		if newOffset > a.cap {
			return 0, ErrArenaFull
		}

		if atomic.CompareAndSwapUint64(&a.offset, curr, newOffset) {
			return uint32(curr + uint64(pad)), nil
		}
		curr = atomic.LoadUint64(&a.offset)
	}
}

func (a *Arena) PutBytes(b []byte) (uint32, error) {
	sz := uint32(len(b))
	offset, err := a.Alloc(sz)
	if err != nil {
		return 0, err
	}
	copy(a.buf[offset:offset+sz], b)
	return offset, nil
}

func (a *Arena) GetBytes(offset uint32, sz uint32) []byte {
	if offset == 0 || uint64(offset)+uint64(sz) > a.cap {
		return nil
	}
	return a.buf[offset : offset+sz]
}

func (a *Arena) Size() uint64 {
	return atomic.LoadUint64(&a.offset)
}

func (a *Arena) Capacity() uint64 {
	return a.cap
}

func (a *Arena) Reset() {
	atomic.StoreUint64(&a.offset, 1)
}
