package raft

import (
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type Role int

const (
	Follower Role = iota
	Candidate
	Leader
)

var (
	ErrNotLeader = errors.New("raft: node is not cluster leader")
)

type LogEntry struct {
	Term  uint64
	Index uint64
	Data  []byte
}

type ApplyMsg struct {
	CommandValid bool
	Command      []byte
	CommandIndex uint64
	CommandTerm  uint64
}

type RequestVoteArgs struct {
	Term         uint64
	CandidateID  uint64
	LastLogIndex uint64
	LastLogTerm  uint64
}

type RequestVoteReply struct {
	Term        uint64
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         uint64
	LeaderID     uint64
	PrevLogIndex uint64
	PrevLogTerm  uint64
	Entries      []LogEntry
	LeaderCommit uint64
}

type AppendEntriesReply struct {
	Term    uint64
	Success bool
}

type Transport interface {
	SendRequestVote(targetNodeID uint64, args *RequestVoteArgs) (*RequestVoteReply, error)
	SendAppendEntries(targetNodeID uint64, args *AppendEntriesArgs) (*AppendEntriesReply, error)
}

type RaftNode struct {
	id        uint64
	peers     []uint64
	transport Transport
	applyCh   chan ApplyMsg

	mu          sync.RWMutex
	currentTerm uint64
	votedFor    uint64
	log         []LogEntry
	role        Role

	commitIndex uint64
	lastApplied uint64

	nextIndex  map[uint64]uint64
	matchIndex map[uint64]uint64

	lastActivity    time.Time
	electionTimeout time.Duration
	stopCh          chan struct{}
	closed          uint32
}

func NewRaftNode(id uint64, peers []uint64, transport Transport, applyCh chan ApplyMsg) *RaftNode {
	rn := &RaftNode{
		id:              id,
		peers:           peers,
		transport:       transport,
		applyCh:         applyCh,
		currentTerm:     0,
		votedFor:        0,
		role:            Follower,
		log:             []LogEntry{{Term: 0, Index: 0}}, // 1-indexed sentinel
		nextIndex:       make(map[uint64]uint64),
		matchIndex:      make(map[uint64]uint64),
		lastActivity:    time.Now(),
		electionTimeout: time.Duration(150+rand.Intn(150)) * time.Millisecond,
		stopCh:          make(chan struct{}),
	}

	go rn.runLoop()
	return rn
}

func (rn *RaftNode) runLoop() {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-rn.stopCh:
			return
		case <-ticker.C:
			rn.mu.Lock()
			if rn.role == Leader {
				rn.broadcastHeartbeatsLocked()
			} else {
				if time.Since(rn.lastActivity) >= rn.electionTimeout {
					rn.startElectionLocked()
				}
			}
			rn.mu.Unlock()
		}
	}
}

func (rn *RaftNode) startElectionLocked() {
	rn.role = Candidate
	rn.currentTerm++
	rn.votedFor = rn.id
	rn.lastActivity = time.Now()
	rn.electionTimeout = time.Duration(150+rand.Intn(150)) * time.Millisecond

	lastLog := rn.log[len(rn.log)-1]
	args := RequestVoteArgs{
		Term:         rn.currentTerm,
		CandidateID:  rn.id,
		LastLogIndex: lastLog.Index,
		LastLogTerm:  lastLog.Term,
	}

	term := rn.currentTerm
	votes := 1
	peers := append([]uint64(nil), rn.peers...)

	for _, peer := range peers {
		if peer == rn.id {
			continue
		}
		go func(target uint64) {
			if rn.transport == nil {
				return
			}
			reply, err := rn.transport.SendRequestVote(target, &args)
			if err != nil || reply == nil {
				return
			}

			rn.mu.Lock()
			defer rn.mu.Unlock()

			if reply.Term > rn.currentTerm {
				rn.currentTerm = reply.Term
				rn.role = Follower
				rn.votedFor = 0
				rn.lastActivity = time.Now()
				return
			}

			if rn.role == Candidate && reply.VoteGranted && rn.currentTerm == term {
				votes++
				if votes > (len(rn.peers)+1)/2 {
					rn.role = Leader
					for _, p := range rn.peers {
						rn.nextIndex[p] = uint64(len(rn.log))
						rn.matchIndex[p] = 0
					}
					rn.broadcastHeartbeatsLocked()
				}
			}
		}(peer)
	}
}

func (rn *RaftNode) broadcastHeartbeatsLocked() {
	if rn.role != Leader {
		return
	}
	term := rn.currentTerm
	leaderID := rn.id
	commitIdx := rn.commitIndex
	peers := append([]uint64(nil), rn.peers...)

	for _, peer := range peers {
		if peer == leaderID {
			continue
		}
		prevIdx := rn.nextIndex[peer] - 1
		if prevIdx >= uint64(len(rn.log)) {
			prevIdx = uint64(len(rn.log) - 1)
		}
		prevTerm := rn.log[prevIdx].Term
		entries := append([]LogEntry(nil), rn.log[prevIdx+1:]...)

		go func(p uint64, pIdx, pTerm uint64, ents []LogEntry) {
			args := AppendEntriesArgs{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: pIdx,
				PrevLogTerm:  pTerm,
				Entries:      ents,
				LeaderCommit: commitIdx,
			}

			if rn.transport == nil {
				return
			}
			reply, err := rn.transport.SendAppendEntries(p, &args)
			if err != nil || reply == nil {
				return
			}

			rn.mu.Lock()
			defer rn.mu.Unlock()

			if reply.Term > rn.currentTerm {
				rn.currentTerm = reply.Term
				rn.role = Follower
				rn.votedFor = 0
				rn.lastActivity = time.Now()
				return
			}

			if rn.role == Leader && reply.Success {
				rn.matchIndex[p] = pIdx + uint64(len(ents))
				rn.nextIndex[p] = rn.matchIndex[p] + 1
				rn.checkCommitQuorumLocked()
			}
		}(peer, prevIdx, prevTerm, entries)
	}
}

func (rn *RaftNode) checkCommitQuorumLocked() {
	for n := uint64(len(rn.log) - 1); n > rn.commitIndex; n-- {
		if rn.log[n].Term != rn.currentTerm {
			continue
		}
		matches := 1
		for _, peer := range rn.peers {
			if peer == rn.id {
				continue
			}
			if rn.matchIndex[peer] >= n {
				matches++
			}
		}
		if matches > (len(rn.peers)+1)/2 {
			rn.commitIndex = n
			rn.applyCommittedLocked()
			break
		}
	}
}

func (rn *RaftNode) applyCommittedLocked() {
	for rn.commitIndex > rn.lastApplied {
		rn.lastApplied++
		entry := rn.log[rn.lastApplied]
		if len(entry.Data) > 0 && rn.applyCh != nil {
			rn.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Data,
				CommandIndex: entry.Index,
				CommandTerm:  entry.Term,
			}
		}
	}
}

func (rn *RaftNode) Propose(command []byte) (uint64, uint64, error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if rn.role != Leader {
		return 0, 0, ErrNotLeader
	}

	index := uint64(len(rn.log))
	term := rn.currentTerm
	rn.log = append(rn.log, LogEntry{
		Term:  term,
		Index: index,
		Data:  command,
	})

	rn.broadcastHeartbeatsLocked()
	return index, term, nil
}

func (rn *RaftNode) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if args.Term < rn.currentTerm {
		reply.Term = rn.currentTerm
		reply.VoteGranted = false
		return
	}

	if args.Term > rn.currentTerm {
		rn.currentTerm = args.Term
		rn.role = Follower
		rn.votedFor = 0
	}

	lastLog := rn.log[len(rn.log)-1]
	upToDate := args.LastLogTerm > lastLog.Term ||
		(args.LastLogTerm == lastLog.Term && args.LastLogIndex >= lastLog.Index)

	if (rn.votedFor == 0 || rn.votedFor == args.CandidateID) && upToDate {
		rn.votedFor = args.CandidateID
		reply.VoteGranted = true
		rn.lastActivity = time.Now()
	} else {
		reply.VoteGranted = false
	}
	reply.Term = rn.currentTerm
}

func (rn *RaftNode) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	if args.Term < rn.currentTerm {
		reply.Term = rn.currentTerm
		reply.Success = false
		return
	}

	rn.lastActivity = time.Now()

	if args.Term > rn.currentTerm || rn.role == Candidate {
		rn.currentTerm = args.Term
		rn.role = Follower
		rn.votedFor = 0
	}

	if args.PrevLogIndex >= uint64(len(rn.log)) || rn.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		reply.Term = rn.currentTerm
		reply.Success = false
		return
	}

	insertIdx := args.PrevLogIndex + 1
	for _, entry := range args.Entries {
		if insertIdx < uint64(len(rn.log)) {
			if rn.log[insertIdx].Term != entry.Term {
				rn.log = rn.log[:insertIdx]
				rn.log = append(rn.log, entry)
			}
		} else {
			rn.log = append(rn.log, entry)
		}
		insertIdx++
	}

	if args.LeaderCommit > rn.commitIndex {
		rn.commitIndex = min(args.LeaderCommit, uint64(len(rn.log)-1))
		rn.applyCommittedLocked()
	}

	reply.Term = rn.currentTerm
	reply.Success = true
}

func (rn *RaftNode) IsLeader() bool {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.role == Leader
}

func (rn *RaftNode) Stop() {
	if atomic.CompareAndSwapUint32(&rn.closed, 0, 1) {
		close(rn.stopCh)
	}
}
