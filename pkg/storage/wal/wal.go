package wal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"project-x/pkg/storage/uring"
)

type writeRequest struct {
	encoded []byte
	err     error
	done    chan struct{}
}

type WAL struct {
	dir         string
	file        *uring.DirectFile
	pool        *uring.AlignedBufferPool
	seq         uint64
	writeQueue  chan *writeRequest
	stopCh      chan struct{}
	wg          sync.WaitGroup
	closed      uint32
	batchWindow time.Duration
	maxBatchSize int
}

func OpenWAL(dir string, batchWindow time.Duration) (*WAL, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	walPath := filepath.Join(dir, "wal.log")
	pool := uring.NewAlignedBufferPool(64 * 1024)

	df, err := uring.OpenDirectFile(walPath, os.O_CREATE|os.O_RDWR, 0644, pool)
	if err != nil {
		return nil, err
	}

	if batchWindow <= 0 {
		batchWindow = 100 * time.Microsecond
	}

	w := &WAL{
		dir:          dir,
		file:         df,
		pool:         pool,
		writeQueue:   make(chan *writeRequest, 10000),
		stopCh:       make(chan struct{}),
		batchWindow:  batchWindow,
		maxBatchSize: 64 * 1024,
	}

	w.wg.Add(1)
	go w.groupCommitLoop()

	return w, nil
}

func (w *WAL) groupCommitLoop() {
	defer w.wg.Done()

	var batch []*writeRequest
	var buf bytes.Buffer
	ticker := time.NewTicker(w.batchWindow)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}

		_, err := w.file.AppendAligned(buf.Bytes())
		if err == nil {
			err = w.file.Sync()
		}

		for _, req := range batch {
			req.err = err
			close(req.done)
		}

		batch = batch[:0]
		buf.Reset()
	}

	for {
		select {
		case <-w.stopCh:
			// Drain remaining requests
			for {
				select {
				case req := <-w.writeQueue:
					batch = append(batch, req)
					buf.Write(req.encoded)
				default:
					flush()
					return
				}
			}

		case req := <-w.writeQueue:
			batch = append(batch, req)
			buf.Write(req.encoded)
			if buf.Len() >= w.maxBatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *WAL) Append(seq uint64, recType byte, key, value []byte) error {
	if atomic.LoadUint32(&w.closed) == 1 {
		return os.ErrClosed
	}

	encoded := EncodeRecord(seq, recType, key, value)
	req := &writeRequest{
		encoded: encoded,
		done:    make(chan struct{}),
	}

	w.writeQueue <- req
	<-req.done
	return req.err
}

func isAllZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func (w *WAL) Recover(fn func(rec *Record) error) (uint64, error) {
	walPath := filepath.Join(w.dir, "wal.log")
	data, err := os.ReadFile(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	offset := 0
	var maxSeq uint64 = 0

	for offset < len(data) {
		remaining := data[offset:]
		checkLen := len(remaining)
		if checkLen > RecordHeaderSize {
			checkLen = RecordHeaderSize
		}

		if isAllZero(remaining[:checkLen]) {
			// Align to next 4KB boundary
			rem := offset % uring.BlockAlignment
			if rem != 0 {
				offset += uring.BlockAlignment - rem
			} else {
				offset += uring.BlockAlignment
			}
			continue
		}

		rec, n, err := DecodeRecord(remaining)
		if err != nil {
			if errors.Is(err, ErrIncompleteRecord) || errors.Is(err, io.EOF) {
				// Clean cutoff at log end
				break
			}
			return maxSeq, err
		}
		rec.Offset = uint64(offset)
		if rec.Seq > maxSeq {
			maxSeq = rec.Seq
		}
		if err := fn(rec); err != nil {
			return maxSeq, err
		}
		offset += n
	}

	return maxSeq, nil
}

func (w *WAL) Close() error {
	if !atomic.CompareAndSwapUint32(&w.closed, 0, 1) {
		return nil
	}

	close(w.stopCh)
	w.wg.Wait()
	return w.file.Close()
}
