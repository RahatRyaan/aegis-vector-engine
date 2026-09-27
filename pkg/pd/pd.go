package pd

import (
	"sync"
	"sync/atomic"
	"time"

	"project-x/pkg/consensus/range"
)

type NodeState int

const (
	NodeOnline NodeState = iota
	NodeSuspect
	NodeOffline
)

type NodeMetadata struct {
	ID            uint64
	Address       string
	LastHeartbeat time.Time
	State         NodeState
	RangeCount    int
	DiskUsedBytes uint64
}

type PlacementDriver struct {
	mu           sync.RWMutex
	nodes        map[uint64]*NodeMetadata
	router       *partition.RangeRouter
	globalSeq    uint64
	nextRangeID  uint64
	heartbeatTTL time.Duration
}

func NewPlacementDriver(router *partition.RangeRouter) *PlacementDriver {
	if router == nil {
		router = partition.NewRangeRouter()
	}
	return &PlacementDriver{
		nodes:        make(map[uint64]*NodeMetadata),
		router:       router,
		globalSeq:    1,
		nextRangeID:  100,
		heartbeatTTL: 5 * time.Second,
	}
}

func (pd *PlacementDriver) NextTSO() uint64 {
	return atomic.AddUint64(&pd.globalSeq, 1)
}

func (pd *PlacementDriver) NextRangeID() uint64 {
	return atomic.AddUint64(&pd.nextRangeID, 1)
}

func (pd *PlacementDriver) RegisterNode(id uint64, address string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.nodes[id] = &NodeMetadata{
		ID:            id,
		Address:       address,
		LastHeartbeat: time.Now(),
		State:         NodeOnline,
	}
}

func (pd *PlacementDriver) Heartbeat(nodeID uint64, rangeCount int, diskBytes uint64) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	n, ok := pd.nodes[nodeID]
	if ok {
		n.LastHeartbeat = time.Now()
		n.State = NodeOnline
		n.RangeCount = rangeCount
		n.DiskUsedBytes = diskBytes
	}
}

func (pd *PlacementDriver) CheckDeadNodes() []uint64 {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	var dead []uint64
	now := time.Now()
	for id, n := range pd.nodes {
		if now.Sub(n.LastHeartbeat) > pd.heartbeatTTL {
			n.State = NodeOffline
			dead = append(dead, id)
		}
	}
	return dead
}

func (pd *PlacementDriver) Router() *partition.RangeRouter {
	return pd.router
}
