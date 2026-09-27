package partition

import (
	"bytes"
	"errors"
	"sync"
)

var (
	ErrKeyOutOfRange = errors.New("range: key does not belong to this range descriptor")
)

type RangeDescriptor struct {
	ID        uint64
	StartKey  []byte
	EndKey    []byte // Empty EndKey indicates positive infinity
	Replicas  []uint64
	LeaderID  uint64
	Size      uint64
	KeyCount  uint64
}

func (rd *RangeDescriptor) Contains(key []byte) bool {
	if bytes.Compare(key, rd.StartKey) < 0 {
		return false
	}
	if len(rd.EndKey) > 0 && bytes.Compare(key, rd.EndKey) >= 0 {
		return false
	}
	return true
}

type RangeRouter struct {
	ranges []*RangeDescriptor
	mu     sync.RWMutex
}

func NewRangeRouter() *RangeRouter {
	return &RangeRouter{
		ranges: []*RangeDescriptor{
			{
				ID:       1,
				StartKey: []byte(""),
				EndKey:   []byte(""), // Initial root range covering entire keyspace
			},
		},
	}
}

func (rr *RangeRouter) Route(key []byte) (*RangeDescriptor, error) {
	rr.mu.RLock()
	defer rr.mu.RUnlock()

	for _, r := range rr.ranges {
		if r.Contains(key) {
			return r, nil
		}
	}
	return nil, ErrKeyOutOfRange
}

func (rr *RangeRouter) SplitRange(rangeID uint64, splitKey []byte, newRangeID uint64) (*RangeDescriptor, *RangeDescriptor, error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	var targetIdx = -1
	for i, r := range rr.ranges {
		if r.ID == rangeID {
			targetIdx = i
			break
		}
	}

	if targetIdx == -1 {
		return nil, nil, errors.New("range: range not found")
	}

	original := rr.ranges[targetIdx]
	if !original.Contains(splitKey) || bytes.Equal(splitKey, original.StartKey) {
		return nil, nil, errors.New("range: invalid split key")
	}

	left := &RangeDescriptor{
		ID:       original.ID,
		StartKey: original.StartKey,
		EndKey:   splitKey,
		Replicas: original.Replicas,
		LeaderID: original.LeaderID,
	}

	right := &RangeDescriptor{
		ID:       newRangeID,
		StartKey: splitKey,
		EndKey:   original.EndKey,
		Replicas: original.Replicas,
		LeaderID: original.LeaderID,
	}

	rr.ranges = append(rr.ranges[:targetIdx], append([]*RangeDescriptor{left, right}, rr.ranges[targetIdx+1:]...)...)
	return left, right, nil
}
