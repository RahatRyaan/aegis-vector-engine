package memtable

import (
	"bytes"
	"encoding/binary"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	maxHeight = 24
)

const (
	OpPut byte = 1
	OpDel byte = 2
)

const (
	offsetKeyOff  = 0
	offsetKeySize = 4
	offsetValOff  = 8
	offsetValSize = 12
	offsetSeq     = 16
	offsetOpType  = 24
	offsetHeight  = 28
	offsetTower   = 32
)

type ConcurrentSkiplist struct {
	arena  *Arena
	head   uint32
	height uint32
	seed   uint64
	count  uint64
}

func NewConcurrentSkiplist(arena *Arena) *ConcurrentSkiplist {
	s := &ConcurrentSkiplist{
		arena:  arena,
		height: 1,
		seed:   uint64(time.Now().UnixNano()),
	}

	headSize := uint32(offsetTower + maxHeight*4)
	headOffset, err := s.arena.Alloc(headSize)
	if err != nil {
		panic("cannot allocate head node for skiplist: " + err.Error())
	}

	s.setNodeHeight(headOffset, maxHeight)
	s.head = headOffset
	return s
}

func (s *ConcurrentSkiplist) fastrand() uint32 {
	v := atomic.AddUint64(&s.seed, 0x9e3779b97f4a7c15)
	v ^= v >> 30
	v *= 0xbf58476d1ce4e5b9
	v ^= v >> 27
	v *= 0x94d049bb133111eb
	v ^= v >> 31
	return uint32(v)
}

func (s *ConcurrentSkiplist) randomHeight() uint16 {
	h := uint16(1)
	for h < maxHeight {
		rnd := s.fastrand()
		// Probability 0.25: lower 2 bits are zero
		if (rnd & 0x3) != 0 {
			break
		}
		h++
	}
	return h
}

func (s *ConcurrentSkiplist) getUint32(offset uint32) uint32 {
	b := s.arena.GetBytes(offset, 4)
	if len(b) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (s *ConcurrentSkiplist) setUint32(offset uint32, val uint32) {
	b := s.arena.GetBytes(offset, 4)
	if len(b) >= 4 {
		binary.LittleEndian.PutUint32(b, val)
	}
}

func (s *ConcurrentSkiplist) getUint64(offset uint32) uint64 {
	b := s.arena.GetBytes(offset, 8)
	if len(b) < 8 {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (s *ConcurrentSkiplist) setUint64(offset uint32, val uint64) {
	b := s.arena.GetBytes(offset, 8)
	if len(b) >= 8 {
		binary.LittleEndian.PutUint64(b, val)
	}
}

func (s *ConcurrentSkiplist) getNextOffset(nodeOffset uint32, h int) uint32 {
	if nodeOffset == 0 {
		return 0
	}
	target := nodeOffset + uint32(offsetTower+h*4)
	b := s.arena.GetBytes(target, 4)
	if len(b) < 4 {
		return 0
	}
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&b[0])))
}

func (s *ConcurrentSkiplist) setNextOffset(nodeOffset uint32, h int, val uint32) {
	if nodeOffset == 0 {
		return
	}
	target := nodeOffset + uint32(offsetTower+h*4)
	b := s.arena.GetBytes(target, 4)
	if len(b) >= 4 {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&b[0])), val)
	}
}

func (s *ConcurrentSkiplist) casNextOffset(nodeOffset uint32, h int, oldVal, newVal uint32) bool {
	if nodeOffset == 0 {
		return false
	}
	target := nodeOffset + uint32(offsetTower+h*4)
	b := s.arena.GetBytes(target, 4)
	if len(b) < 4 {
		return false
	}
	return atomic.CompareAndSwapUint32((*uint32)(unsafe.Pointer(&b[0])), oldVal, newVal)
}

func (s *ConcurrentSkiplist) setNodeHeight(nodeOffset uint32, h uint32) {
	s.setUint32(nodeOffset+offsetHeight, h)
}

func (s *ConcurrentSkiplist) getNodeHeight(nodeOffset uint32) uint32 {
	return s.getUint32(nodeOffset + offsetHeight)
}

func (s *ConcurrentSkiplist) nodeKey(nodeOffset uint32) []byte {
	if nodeOffset == 0 {
		return nil
	}
	kOff := s.getUint32(nodeOffset + offsetKeyOff)
	kSize := s.getUint32(nodeOffset + offsetKeySize)
	return s.arena.GetBytes(kOff, kSize)
}

func (s *ConcurrentSkiplist) nodeValue(nodeOffset uint32) []byte {
	if nodeOffset == 0 {
		return nil
	}
	vOff := s.getUint32(nodeOffset + offsetValOff)
	vSize := s.getUint32(nodeOffset + offsetValSize)
	return s.arena.GetBytes(vOff, vSize)
}

func (s *ConcurrentSkiplist) nodeSeq(nodeOffset uint32) uint64 {
	if nodeOffset == 0 {
		return 0
	}
	return s.getUint64(nodeOffset + offsetSeq)
}

func (s *ConcurrentSkiplist) nodeOpType(nodeOffset uint32) byte {
	if nodeOffset == 0 {
		return 0
	}
	return byte(s.getUint32(nodeOffset + offsetOpType))
}

func (s *ConcurrentSkiplist) compareKey(key []byte, nodeOffset uint32) int {
	if nodeOffset == 0 || nodeOffset == s.head {
		return 1
	}
	nk := s.nodeKey(nodeOffset)
	return bytes.Compare(key, nk)
}

func (s *ConcurrentSkiplist) findGreaterOrEqual(key []byte, prev *[maxHeight]uint32) uint32 {
	curr := s.head
	lvl := int(atomic.LoadUint32(&s.height)) - 1

	for {
		next := s.getNextOffset(curr, lvl)
		for next != 0 && s.compareKey(key, next) > 0 {
			curr = next
			next = s.getNextOffset(curr, lvl)
		}

		if prev != nil {
			prev[lvl] = curr
		}

		if lvl == 0 {
			return next
		}
		lvl--
	}
}

func (s *ConcurrentSkiplist) Put(key, value []byte, op byte, seq uint64) error {
	var prev [maxHeight]uint32
	_ = s.findGreaterOrEqual(key, &prev)

	h := s.randomHeight()
	currHeight := atomic.LoadUint32(&s.height)
	for uint32(h) > currHeight {
		if atomic.CompareAndSwapUint32(&s.height, currHeight, uint32(h)) {
			break
		}
		currHeight = atomic.LoadUint32(&s.height)
	}

	for i := int(currHeight); i < int(h); i++ {
		prev[i] = s.head
	}

	keyOff, err := s.arena.PutBytes(key)
	if err != nil {
		return err
	}

	valOff := uint32(0)
	if len(value) > 0 {
		valOff, err = s.arena.PutBytes(value)
		if err != nil {
			return err
		}
	}

	nodeSz := uint32(offsetTower + int(h)*4)
	nOffset, err := s.arena.Alloc(nodeSz)
	if err != nil {
		return err
	}

	s.setUint32(nOffset+offsetKeyOff, keyOff)
	s.setUint32(nOffset+offsetKeySize, uint32(len(key)))
	s.setUint32(nOffset+offsetValOff, valOff)
	s.setUint32(nOffset+offsetValSize, uint32(len(value)))
	s.setUint64(nOffset+offsetSeq, seq)
	s.setUint32(nOffset+offsetOpType, uint32(op))
	s.setNodeHeight(nOffset, uint32(h))

	for i := 0; i < int(h); i++ {
		for {
			prevNodeOffset := prev[i]
			if prevNodeOffset == 0 {
				prevNodeOffset = s.head
			}
			nextOffset := s.getNextOffset(prevNodeOffset, i)
			s.setNextOffset(nOffset, i, nextOffset)

			if s.casNextOffset(prevNodeOffset, i, nextOffset, nOffset) {
				break
			}
			_ = s.findGreaterOrEqual(key, &prev)
		}
	}

	atomic.AddUint64(&s.count, 1)
	return nil
}

func (s *ConcurrentSkiplist) Get(key []byte) ([]byte, uint64, byte, bool) {
	next := s.findGreaterOrEqual(key, nil)
	if next == 0 {
		return nil, 0, 0, false
	}
	if bytes.Equal(s.nodeKey(next), key) {
		return s.nodeValue(next), s.nodeSeq(next), s.nodeOpType(next), true
	}
	return nil, 0, 0, false
}

func (s *ConcurrentSkiplist) Count() uint64 {
	return atomic.LoadUint64(&s.count)
}

func (s *ConcurrentSkiplist) Arena() *Arena {
	return s.arena
}
