package raft

import (
	"sync"
	"testing"
	"time"
)

type LocalNetworkTransport struct {
	mu    sync.RWMutex
	nodes map[uint64]*RaftNode
}

func NewLocalNetworkTransport() *LocalNetworkTransport {
	return &LocalNetworkTransport{
		nodes: make(map[uint64]*RaftNode),
	}
}

func (t *LocalNetworkTransport) Register(id uint64, node *RaftNode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[id] = node
}

func (t *LocalNetworkTransport) SendRequestVote(target uint64, args *RequestVoteArgs) (*RequestVoteReply, error) {
	t.mu.RLock()
	node, ok := t.nodes[target]
	t.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	reply := &RequestVoteReply{}
	node.RequestVote(args, reply)
	return reply, nil
}

func (t *LocalNetworkTransport) SendAppendEntries(target uint64, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	t.mu.RLock()
	node, ok := t.nodes[target]
	t.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	reply := &AppendEntriesReply{}
	node.AppendEntries(args, reply)
	return reply, nil
}

func TestRaft3NodeElectionAndReplication(t *testing.T) {
	transport := NewLocalNetworkTransport()
	peers := []uint64{1, 2, 3}

	applyCh1 := make(chan ApplyMsg, 100)
	applyCh2 := make(chan ApplyMsg, 100)
	applyCh3 := make(chan ApplyMsg, 100)

	node1 := NewRaftNode(1, peers, transport, applyCh1)
	node2 := NewRaftNode(2, peers, transport, applyCh2)
	node3 := NewRaftNode(3, peers, transport, applyCh3)

	transport.Register(1, node1)
	transport.Register(2, node2)
	transport.Register(3, node3)

	defer func() {
		node1.Stop()
		node2.Stop()
		node3.Stop()
	}()

	// Wait for leader election (timeout up to 2 seconds)
	var leader *RaftNode
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if node1.IsLeader() {
			leader = node1
			break
		}
		if node2.IsLeader() {
			leader = node2
			break
		}
		if node3.IsLeader() {
			leader = node3
			break
		}
	}

	if leader == nil {
		t.Fatalf("no leader elected among 3 nodes within 2 seconds")
	}

	// Propose command on leader
	cmd := []byte("SET account_42_balance 100000")
	idx, term, err := leader.Propose(cmd)
	if err != nil {
		t.Fatalf("leader failed to propose command: %v", err)
	}
	if idx != 1 || term == 0 {
		t.Fatalf("unexpected idx or term: idx=%d, term=%d", idx, term)
	}

	// Allow log replication
	time.Sleep(200 * time.Millisecond)
}
