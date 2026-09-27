package multiraft

import (
	"errors"
	"sync"

	"project-x/pkg/consensus/raft"
	"project-x/pkg/consensus/range"
)

type MultiRaftEngine struct {
	nodeID    uint64
	transport raft.Transport
	groups    sync.Map // map[uint64]*raft.RaftNode (rangeID -> RaftNode)
	router    *partition.RangeRouter
}

func NewMultiRaftEngine(nodeID uint64, transport raft.Transport, router *partition.RangeRouter) *MultiRaftEngine {
	return &MultiRaftEngine{
		nodeID:    nodeID,
		transport: transport,
		router:    router,
	}
}

func (m *MultiRaftEngine) CreateGroup(rangeID uint64, peers []uint64, applyCh chan raft.ApplyMsg) *raft.RaftNode {
	rn := raft.NewRaftNode(m.nodeID, peers, m.transport, applyCh)
	m.groups.Store(rangeID, rn)
	return rn
}

func (m *MultiRaftEngine) GetGroup(rangeID uint64) (*raft.RaftNode, bool) {
	val, ok := m.groups.Load(rangeID)
	if !ok {
		return nil, false
	}
	return val.(*raft.RaftNode), true
}

func (m *MultiRaftEngine) ProposeForKey(key, command []byte) (uint64, uint64, error) {
	desc, err := m.router.Route(key)
	if err != nil {
		return 0, 0, err
	}

	rn, ok := m.GetGroup(desc.ID)
	if !ok {
		return 0, 0, errors.New("multiraft: raft group not running on this node")
	}

	return rn.Propose(command)
}

func (m *MultiRaftEngine) StopAll() {
	m.groups.Range(func(key, value interface{}) bool {
		rn := value.(*raft.RaftNode)
		rn.Stop()
		return true
	})
}
