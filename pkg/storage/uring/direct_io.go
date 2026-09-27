package uring

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	ErrUnalignedBuffer = errors.New("directio: buffer address or length not 4KB aligned")
	ErrShortWrite      = errors.New("directio: wrote fewer bytes than expected")
)

type DirectFile struct {
	file       *os.File
	fd         int
	path       string
	offset     uint64
	directMode bool
	mu         sync.Mutex
	pool       *AlignedBufferPool
}

func OpenDirectFile(path string, flag int, perm os.FileMode, pool *AlignedBufferPool) (*DirectFile, error) {
	if pool == nil {
		pool = NewAlignedBufferPool(DefaultBlockSize)
	}

	directFlag := flag | unix.O_DIRECT
	f, err := os.OpenFile(path, directFlag, perm)
	directMode := true

	if err != nil {
		// Fallback to buffered/synced IO if O_DIRECT is not supported on this mount
		f, err = os.OpenFile(path, flag|unix.O_SYNC, perm)
		if err != nil {
			return nil, err
		}
		directMode = false
	}

	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	df := &DirectFile{
		file:       f,
		fd:         int(f.Fd()),
		path:       path,
		offset:     uint64(stat.Size()),
		directMode: directMode,
		pool:       pool,
	}

	return df, nil
}

func (df *DirectFile) WriteDirectAt(buf []byte, offset uint64) (int, error) {
	if df.directMode {
		if !IsAligned(buf) || len(buf)%BlockAlignment != 0 || offset%BlockAlignment != 0 {
			return 0, ErrUnalignedBuffer
		}
		n, err := unix.Pwrite(df.fd, buf, int64(offset))
		if err != nil {
			return 0, err
		}
		if n < len(buf) {
			return n, ErrShortWrite
		}
		return n, nil
	}

	// Fallback path
	df.mu.Lock()
	defer df.mu.Unlock()
	n, err := df.file.WriteAt(buf, int64(offset))
	return n, err
}

func (df *DirectFile) ReadDirectAt(buf []byte, offset uint64) (int, error) {
	if df.directMode {
		if !IsAligned(buf) || len(buf)%BlockAlignment != 0 || offset%BlockAlignment != 0 {
			return 0, ErrUnalignedBuffer
		}
		return unix.Pread(df.fd, buf, int64(offset))
	}

	df.mu.Lock()
	defer df.mu.Unlock()
	return df.file.ReadAt(buf, int64(offset))
}

func (df *DirectFile) AppendAligned(data []byte) (uint64, error) {
	df.mu.Lock()
	defer df.mu.Unlock()

	dataLen := len(data)
	if dataLen == 0 {
		return df.offset, nil
	}

	alignedLen := dataLen
	if rem := alignedLen % BlockAlignment; rem != 0 {
		alignedLen += BlockAlignment - rem
	}

	buf := df.pool.Get()
	defer df.pool.Put(buf)

	if len(buf) < alignedLen {
		buf = AllocAligned(alignedLen)
	}

	copy(buf[:dataLen], data)

	currOffset := df.offset
	if df.directMode {
		alignedOffset := currOffset
		if rem := alignedOffset % BlockAlignment; rem != 0 {
			alignedOffset += BlockAlignment - rem
		}

		n, err := unix.Pwrite(df.fd, buf[:alignedLen], int64(alignedOffset))
		if err != nil {
			return 0, err
		}
		if n < alignedLen {
			return 0, ErrShortWrite
		}
		df.offset = alignedOffset + uint64(dataLen)
		return currOffset, nil
	}

	n, err := df.file.WriteAt(data, int64(currOffset))
	if err != nil {
		return 0, err
	}
	df.offset += uint64(n)
	return currOffset, nil
}

func (df *DirectFile) Sync() error {
	if df.directMode {
		return nil // O_DIRECT with hardware cache commit
	}
	return df.file.Sync()
}

func (df *DirectFile) Size() uint64 {
	return atomic.LoadUint64(&df.offset)
}

func (df *DirectFile) IsDirect() bool {
	return df.directMode
}

func (df *DirectFile) Close() error {
	df.mu.Lock()
	defer df.mu.Unlock()
	_ = syscall.Fsync(df.fd)
	return df.file.Close()
}
