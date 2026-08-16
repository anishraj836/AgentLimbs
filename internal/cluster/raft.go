package cluster

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	mrand "math/rand"
	"sync"
	"time"
)

// RaftRole represents the current consensus state of a Raft node.
type RaftRole string

const (
	RoleFollower  RaftRole = "Follower"
	RoleCandidate RaftRole = "Candidate"
	RoleLeader    RaftRole = "Leader"
)

// RaftCommandType denotes the operational category of a log entry.
type RaftCommandType string

const (
	CmdIndexDocument  RaftCommandType = "INDEX_DOC"
	CmdDeleteDocument RaftCommandType = "DELETE_DOC"
	CmdSetPrecision   RaftCommandType = "SET_PRECISION"
)

// RaftLogEntry represents an individual log record in the consensus history.
type RaftLogEntry struct {
	Index   uint64          `json:"index"`
	Term    uint64          `json:"term"`
	Type    RaftCommandType `json:"type"`
	Payload []byte          `json:"payload"`
}

// RequestVoteArgs holds parameters for RequestVote RPCs.
type RequestVoteArgs struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteReply holds the response from RequestVote RPCs.
type RequestVoteReply struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
}

// AppendEntriesArgs holds parameters for AppendEntries RPCs.
type AppendEntriesArgs struct {
	Term         uint64         `json:"term"`
	LeaderID     string         `json:"leader_id"`
	PrevLogIndex uint64         `json:"prev_log_index"`
	PrevLogTerm  uint64         `json:"prev_log_term"`
	Entries      []RaftLogEntry `json:"entries"`
	LeaderCommit uint64         `json:"leader_commit"`
}

// AppendEntriesReply holds the response from AppendEntries RPCs.
type AppendEntriesReply struct {
	Term          uint64 `json:"term"`
	Success       bool   `json:"success"`
	ConflictIndex uint64 `json:"conflict_index,omitempty"`
	ConflictTerm  uint64 `json:"conflict_term,omitempty"`
}

// InstallSnapshotArgs holds parameters for InstallSnapshot RPCs.
type InstallSnapshotArgs struct {
	Term              uint64 `json:"term"`
	LeaderID          string `json:"leader_id"`
	LastIncludedIndex uint64 `json:"last_included_index"`
	LastIncludedTerm  uint64 `json:"last_included_term"`
	Data              []byte `json:"data"`
}

// InstallSnapshotReply holds the response from InstallSnapshot RPCs.
type InstallSnapshotReply struct {
	Term uint64 `json:"term"`
}

// RaftTransport defines the interface for communicating with peer nodes.
type RaftTransport interface {
	RequestVote(ctx context.Context, peer string, args *RequestVoteArgs) (*RequestVoteReply, error)
	AppendEntries(ctx context.Context, peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error)
	InstallSnapshot(ctx context.Context, peer string, args *InstallSnapshotArgs) (*InstallSnapshotReply, error)
}

// RaftConfig configures the operational timeouts and parameters of a Raft node.
type RaftConfig struct {
	NodeID            string
	Peers             []string
	HeartbeatInterval time.Duration
	MinElectionTimeout time.Duration
	MaxElectionTimeout time.Duration
}

// DefaultRaftConfig creates standard production timing parameters.
func DefaultRaftConfig(nodeID string, peers []string) RaftConfig {
	return RaftConfig{
		NodeID:            nodeID,
		Peers:             peers,
		HeartbeatInterval: 50 * time.Millisecond,
		MinElectionTimeout: 150 * time.Millisecond,
		MaxElectionTimeout: 300 * time.Millisecond,
	}
}

// RaftNode represents a single consensus actor in the distributed cluster.
type RaftNode struct {
	mu        sync.RWMutex
	cfg       RaftConfig
	transport RaftTransport
	applyCh   chan ApplyMsg
	stopCh    chan struct{}
	rng       *mrand.Rand

	// Persistent state
	currentTerm uint64
	votedFor    string
	log         []RaftLogEntry // 0-indexed internally, with dummy entry at 0

	// Snapshot state
	lastIncludedIndex uint64
	lastIncludedTerm  uint64

	// Volatile state
	role        RaftRole
	commitIndex uint64
	lastApplied uint64
	leaderID    string

	// Leader volatile state
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// Proposal futures
	proposals map[uint64]chan error

	// FIFO applicator queue
	applyTasks chan applyTask

	// Timers
	electionTimer   *time.Timer
	heartbeatTicker *time.Ticker
}

type applyTask struct {
	msg    ApplyMsg
	future chan error
}

// NewRaftNode creates and initializes a Raft consensus node.
func NewRaftNode(cfg RaftConfig, transport RaftTransport, applyCh chan ApplyMsg) *RaftNode {
	var seed int64
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		seed = int64(binary.LittleEndian.Uint64(b[:]))
	} else {
		seed = time.Now().UnixNano()
	}

	rn := &RaftNode{
		cfg:         cfg,
		transport:   transport,
		applyCh:     applyCh,
		stopCh:      make(chan struct{}),
		rng:         mrand.New(mrand.NewSource(seed)),
		role:        RoleFollower,
		currentTerm: 0,
		votedFor:    "",
		log: []RaftLogEntry{
			{Index: 0, Term: 0}, // Base dummy entry
		},
		nextIndex:  make(map[string]uint64),
		matchIndex: make(map[string]uint64),
		proposals:  make(map[uint64]chan error),
		applyTasks: make(chan applyTask, 1024),
	}

	rn.resetElectionTimer()
	go rn.runLifecycle()
	go rn.applyLoop()

	return rn
}

// getRandomElectionTimeout returns a jittered duration in [MinElectionTimeout, MaxElectionTimeout].
func (rn *RaftNode) getRandomElectionTimeout() time.Duration {
	min := rn.cfg.MinElectionTimeout
	max := rn.cfg.MaxElectionTimeout
	if max <= min {
		max = min + 150*time.Millisecond
	}
	delta := max - min
	return min + time.Duration(rn.rng.Int63n(int64(delta)))
}

func (rn *RaftNode) resetElectionTimer() {
	dur := rn.getRandomElectionTimeout()
	if rn.electionTimer == nil {
		rn.electionTimer = time.NewTimer(dur)
	} else {
		rn.electionTimer.Reset(dur)
	}
}

// runLifecycle drives the background election timeouts and heartbeat loops.
func (rn *RaftNode) runLifecycle() {
	rn.mu.Lock()
	rn.heartbeatTicker = time.NewTicker(rn.cfg.HeartbeatInterval)
	rn.mu.Unlock()

	for {
		select {
		case <-rn.stopCh:
			return

		case <-rn.electionTimer.C:
			rn.mu.Lock()
			if rn.role != RoleLeader {
				rn.startElectionLocked()
			} else {
				rn.resetElectionTimer()
			}
			rn.mu.Unlock()

		case <-rn.heartbeatTicker.C:
			rn.mu.Lock()
			if rn.role == RoleLeader {
				rn.broadcastAppendEntriesLocked()
			}
			rn.mu.Unlock()
		}
	}
}

// lastLogInfo returns the (index, term) of the most recent log entry.
func (rn *RaftNode) lastLogInfo() (uint64, uint64) {
	last := rn.log[len(rn.log)-1]
	return last.Index, last.Term
}

func (rn *RaftNode) totalClusterNodes() int {
	nodes := make(map[string]bool)
	nodes[rn.cfg.NodeID] = true
	for _, p := range rn.cfg.Peers {
		if p != "" {
			nodes[p] = true
		}
	}
	return len(nodes)
}

func (rn *RaftNode) quorumSize() int {
	total := rn.totalClusterNodes()
	return (total / 2) + 1
}

func (rn *RaftNode) isSoloCluster() bool {
	return rn.totalClusterNodes() <= 1
}

// startElectionLocked converts the node to Candidate and requests votes. Caller must hold rn.mu.
func (rn *RaftNode) startElectionLocked() {
	rn.role = RoleCandidate
	rn.currentTerm++
	rn.votedFor = rn.cfg.NodeID
	rn.leaderID = ""
	rn.resetElectionTimer()

	// In single-node cluster, become leader immediately
	if rn.isSoloCluster() {
		rn.becomeLeaderLocked()
		return
	}

	term := rn.currentTerm
	lastIndex, lastTerm := rn.lastLogInfo()
	peers := make([]string, len(rn.cfg.Peers))
	copy(peers, rn.cfg.Peers)

	votes := 1 // Vote for self
	var voteMu sync.Mutex

	for _, peer := range peers {
		if peer == rn.cfg.NodeID {
			continue
		}
		go func(p string) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()

			args := &RequestVoteArgs{
				Term:         term,
				CandidateID:  rn.cfg.NodeID,
				LastLogIndex: lastIndex,
				LastLogTerm:  lastTerm,
			}

			reply, err := rn.transport.RequestVote(ctx, p, args)
			if err != nil || reply == nil {
				return
			}

			rn.mu.Lock()
			defer rn.mu.Unlock()

			// Universal higher term step-down (§5.1)
			if reply.Term > rn.currentTerm {
				rn.currentTerm = reply.Term
				rn.role = RoleFollower
				rn.votedFor = ""
				rn.leaderID = ""
				rn.resetElectionTimer()
				rn.failPendingProposalsLocked(ErrNotLeader)
				return
			}

			if rn.role == RoleCandidate && rn.currentTerm == term && reply.VoteGranted {
				voteMu.Lock()
				votes++
				currentVotes := votes
				voteMu.Unlock()

				quorum := rn.quorumSize()
				if currentVotes >= quorum && rn.role == RoleCandidate {
					rn.becomeLeaderLocked()
				}
			}
		}(peer)
	}
}

// becomeLeaderLocked initializes leader volatile state and broadcasts heartbeats. Caller must hold rn.mu.
func (rn *RaftNode) becomeLeaderLocked() {
	rn.role = RoleLeader
	rn.leaderID = rn.cfg.NodeID
	lastIdx, _ := rn.lastLogInfo()

	for _, peer := range rn.cfg.Peers {
		rn.nextIndex[peer] = lastIdx + 1
		rn.matchIndex[peer] = 0
	}

	rn.broadcastAppendEntriesLocked()
}

// broadcastAppendEntriesLocked replicates log entries to all peers. Caller must hold rn.mu.
func (rn *RaftNode) broadcastAppendEntriesLocked() {
	if rn.role != RoleLeader {
		return
	}

	term := rn.currentTerm
	leaderCommit := rn.commitIndex
	leaderID := rn.cfg.NodeID

	for _, peer := range rn.cfg.Peers {
		if peer == rn.cfg.NodeID {
			continue
		}

		nextIdx := rn.nextIndex[peer]
		if nextIdx == 0 {
			nextIdx = 1
		}

		// Handle InstallSnapshot if peer is lagging beyond log base
		if nextIdx <= rn.lastIncludedIndex {
			// Lagging peer snapshot replication
			continue
		}

		prevLogIndex := nextIdx - 1
		prevLogTerm := rn.getLogTerm(prevLogIndex)

		// Slice entries to send
		var entries []RaftLogEntry
		if prevLogIndex < uint64(len(rn.log)-1) {
			entries = make([]RaftLogEntry, len(rn.log[prevLogIndex+1:]))
			copy(entries, rn.log[prevLogIndex+1:])
		}

		go func(p string, pLogIdx, pLogTerm, nIdx uint64, ents []RaftLogEntry) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()

			args := &AppendEntriesArgs{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: pLogIdx,
				PrevLogTerm:  pLogTerm,
				Entries:      ents,
				LeaderCommit: leaderCommit,
			}

			reply, err := rn.transport.AppendEntries(ctx, p, args)
			if err != nil || reply == nil {
				return
			}

			rn.mu.Lock()
			defer rn.mu.Unlock()

			// Universal higher term step-down (§5.1)
			if reply.Term > rn.currentTerm {
				rn.currentTerm = reply.Term
				rn.role = RoleFollower
				rn.votedFor = ""
				rn.leaderID = ""
				rn.resetElectionTimer()
				rn.failPendingProposalsLocked(ErrNotLeader)
				return
			}

			if rn.role != RoleLeader || rn.currentTerm != term {
				return
			}

			if reply.Success {
				newNext := pLogIdx + uint64(len(ents)) + 1
				newMatch := pLogIdx + uint64(len(ents))
				if newNext > rn.nextIndex[p] {
					rn.nextIndex[p] = newNext
				}
				if newMatch > rn.matchIndex[p] {
					rn.matchIndex[p] = newMatch
				}
				rn.advanceCommitIndexLocked()
			} else {
				// Decrement nextIndex and retry on conflict
				if reply.ConflictIndex > 0 {
					rn.nextIndex[p] = reply.ConflictIndex
				} else if rn.nextIndex[p] > 1 {
					rn.nextIndex[p]--
				}
			}
		}(peer, prevLogIndex, prevLogTerm, nextIdx, entries)
	}
}

func (rn *RaftNode) getLogTerm(index uint64) uint64 {
	if index == rn.lastIncludedIndex {
		return rn.lastIncludedTerm
	}
	if index < uint64(len(rn.log)) {
		return rn.log[index].Term
	}
	return 0
}

// advanceCommitIndexLocked advances commitIndex based on majority matchIndex and term check (§5.4.2).
func (rn *RaftNode) advanceCommitIndexLocked() {
	lastIdx, _ := rn.lastLogInfo()

	for N := lastIdx; N > rn.commitIndex; N-- {
		// Strictly enforce Ongaro §5.4.2: entry must be from the current term
		if rn.log[N].Term != rn.currentTerm {
			continue
		}

		matches := 1 // Leader has matched
		for _, peer := range rn.cfg.Peers {
			if peer == rn.cfg.NodeID {
				continue
			}
			if rn.matchIndex[peer] >= N {
				matches++
			}
		}

		quorum := rn.quorumSize()
		if matches >= quorum {
			rn.commitIndex = N
			rn.applyCommittedEntriesLocked()
			break
		}
	}
}

// applyCommittedEntriesLocked dispatches newly committed log entries onto the FIFO applyTasks queue.
func (rn *RaftNode) applyCommittedEntriesLocked() {
	for rn.commitIndex > rn.lastApplied {
		rn.lastApplied++
		entry := rn.log[rn.lastApplied]

		var future chan error
		if ch, exists := rn.proposals[entry.Index]; exists {
			future = ch
			delete(rn.proposals, entry.Index)
		}

		task := applyTask{
			msg: ApplyMsg{
				CommandValid: true,
				Command:      entry,
				CommandIndex: entry.Index,
			},
			future: future,
		}

		select {
		case rn.applyTasks <- task:
		default:
			go func(t applyTask) {
				select {
				case <-rn.stopCh:
					if t.future != nil {
						t.future <- ErrNotLeader
						close(t.future)
					}
					return
				case rn.applyTasks <- t:
				}
			}(task)
		}
	}
}

// applyLoop runs in a single dedicated background goroutine to deliver committed
// log messages to applyCh in strict monotonic sequence without holding rn.mu.
func (rn *RaftNode) applyLoop() {
	for {
		select {
		case <-rn.stopCh:
			return
		case task := <-rn.applyTasks:
			rn.applyCh <- task.msg
			if task.future != nil {
				task.future <- nil
				close(task.future)
			}
		}
	}
}

func (rn *RaftNode) failPendingProposalsLocked(err error) {
	for idx, ch := range rn.proposals {
		ch <- err
		close(ch)
		delete(rn.proposals, idx)
	}
}

// RequestVote handles incoming RequestVote RPCs.
func (rn *RaftNode) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	reply.Term = rn.currentTerm
	reply.VoteGranted = false

	if args.Term < rn.currentTerm {
		return nil
	}

	// Universal higher term step-down (§5.1)
	if args.Term > rn.currentTerm {
		rn.currentTerm = args.Term
		rn.role = RoleFollower
		rn.votedFor = ""
		rn.leaderID = ""
		rn.resetElectionTimer()
		rn.failPendingProposalsLocked(ErrNotLeader)
	}

	reply.Term = rn.currentTerm

	// Check if already voted for someone else in this term
	canVote := (rn.votedFor == "" || rn.votedFor == args.CandidateID)

	// Strict Ongaro §5.4.1 Up-to-Date Log Check
	lastIndex, lastTerm := rn.lastLogInfo()
	isUpToDate := (args.LastLogTerm > lastTerm) ||
		(args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIndex)

	if canVote && isUpToDate {
		rn.votedFor = args.CandidateID
		reply.VoteGranted = true
		rn.resetElectionTimer()
	}

	return nil
}

// AppendEntries handles incoming AppendEntries RPCs.
func (rn *RaftNode) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	reply.Term = rn.currentTerm
	reply.Success = false

	if args.Term < rn.currentTerm {
		return nil
	}

	// Universal higher term step-down or leader recognition
	if args.Term > rn.currentTerm || rn.role == RoleCandidate {
		rn.currentTerm = args.Term
		rn.role = RoleFollower
		rn.votedFor = ""
		rn.resetElectionTimer()
		rn.failPendingProposalsLocked(ErrNotLeader)
	}

	rn.leaderID = args.LeaderID
	rn.resetElectionTimer()
	reply.Term = rn.currentTerm

	lastIdx, _ := rn.lastLogInfo()

	// 1. Log matching check: PrevLogIndex must exist
	if args.PrevLogIndex > lastIdx {
		reply.ConflictIndex = lastIdx + 1
		return nil
	}

	// 2. Term at PrevLogIndex must match PrevLogTerm
	if rn.getLogTerm(args.PrevLogIndex) != args.PrevLogTerm {
		reply.ConflictTerm = rn.getLogTerm(args.PrevLogIndex)
		// Find first index of conflicting term
		for idx := uint64(1); idx <= args.PrevLogIndex; idx++ {
			if rn.log[idx].Term == reply.ConflictTerm {
				reply.ConflictIndex = idx
				break
			}
		}
		return nil
	}

	// 3. Point-of-divergence log append (do not indiscriminately truncate)
	for i, entry := range args.Entries {
		entryIdx := args.PrevLogIndex + 1 + uint64(i)
		if entryIdx < uint64(len(rn.log)) {
			if rn.log[entryIdx].Term != entry.Term {
				// Conflicting entry found: truncate from here
				rn.log = rn.log[:entryIdx]
				rn.log = append(rn.log, entry)
			}
		} else {
			// Non-conflicting new entry: append directly
			rn.log = append(rn.log, entry)
		}
	}

	reply.Success = true

	// 4. Update commit index
	if args.LeaderCommit > rn.commitIndex {
		lastNewEntryIdx, _ := rn.lastLogInfo()
		if args.LeaderCommit < lastNewEntryIdx {
			rn.commitIndex = args.LeaderCommit
		} else {
			rn.commitIndex = lastNewEntryIdx
		}
		rn.applyCommittedEntriesLocked()
	}

	return nil
}

// Propose submits a new command to the Raft cluster and blocks until quorum commit.
func (rn *RaftNode) Propose(ctx context.Context, cmdType RaftCommandType, payload []byte) (uint64, uint64, error) {
	rn.mu.Lock()

	if rn.role != RoleLeader {
		leader := rn.leaderID
		rn.mu.Unlock()
		if leader != "" {
			return 0, 0, fmt.Errorf("%w (leader is %s)", ErrNotLeader, leader)
		}
		return 0, 0, ErrNotLeader
	}

	lastIdx, _ := rn.lastLogInfo()
	newIdx := lastIdx + 1
	term := rn.currentTerm

	entry := RaftLogEntry{
		Index:   newIdx,
		Term:    term,
		Type:    cmdType,
		Payload: payload,
	}

	rn.log = append(rn.log, entry)

	futureCh := make(chan error, 1)
	rn.proposals[newIdx] = futureCh

	// If single node cluster, commit immediately
	if len(rn.cfg.Peers) == 0 {
		rn.commitIndex = newIdx
		rn.applyCommittedEntriesLocked()
		rn.mu.Unlock()
		return newIdx, term, nil
	}

	rn.broadcastAppendEntriesLocked()
	rn.mu.Unlock()

	select {
	case <-ctx.Done():
		rn.mu.Lock()
		delete(rn.proposals, newIdx)
		rn.mu.Unlock()
		return 0, 0, ctx.Err()

	case err, ok := <-futureCh:
		if !ok || err != nil {
			if err == nil {
				err = ErrCommandAborted
			}
			return 0, 0, err
		}
		return newIdx, term, nil
	}
}

// Status returns current node telemetry for health checks and API inspection.
func (rn *RaftNode) Status() (role RaftRole, term uint64, leaderID string, commitIdx uint64, lastLogIdx uint64) {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	lastIdx, _ := rn.lastLogInfo()
	return rn.role, rn.currentTerm, rn.leaderID, rn.commitIndex, lastIdx
}

// IsLeader checks whether this node is currently the cluster leader.
func (rn *RaftNode) IsLeader() bool {
	rn.mu.RLock()
	defer rn.mu.RUnlock()
	return rn.role == RoleLeader
}

// Close gracefully terminates the Raft node lifecycle.
func (rn *RaftNode) Close() {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	select {
	case <-rn.stopCh:
		return
	default:
		close(rn.stopCh)
	}

	if rn.electionTimer != nil {
		rn.electionTimer.Stop()
	}
	if rn.heartbeatTicker != nil {
		rn.heartbeatTicker.Stop()
	}

	rn.failPendingProposalsLocked(errors.New("cluster: raft node closed"))
}
