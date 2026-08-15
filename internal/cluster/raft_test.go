package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/crawler-monorepo/internal/index"
)

// MockNetworkTransport simulates an in-memory network interconnecting Raft nodes.
type MockNetworkTransport struct {
	mu          sync.RWMutex
	nodes       map[string]*RaftNode
	partitioned map[string]bool // node -> is partitioned/isolated
}

func NewMockNetworkTransport() *MockNetworkTransport {
	return &MockNetworkTransport{
		nodes:       make(map[string]*RaftNode),
		partitioned: make(map[string]bool),
	}
}

func (m *MockNetworkTransport) Register(nodeID string, rn *RaftNode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[nodeID] = rn
}

func (m *MockNetworkTransport) Partition(nodeID string, isolated bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.partitioned[nodeID] = isolated
}

func (m *MockNetworkTransport) isIsolated(from, to string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.partitioned[from] || m.partitioned[to]
}

func (m *MockNetworkTransport) RequestVote(ctx context.Context, peer string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	if m.isIsolated(args.CandidateID, peer) {
		return nil, fmt.Errorf("network partition between %s and %s", args.CandidateID, peer)
	}

	m.mu.RLock()
	target, ok := m.nodes[peer]
	m.mu.RUnlock()

	if !ok || target == nil {
		return nil, fmt.Errorf("node %s not found", peer)
	}

	var reply RequestVoteReply
	err := target.RequestVote(args, &reply)
	return &reply, err
}

func (m *MockNetworkTransport) AppendEntries(ctx context.Context, peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	if m.isIsolated(args.LeaderID, peer) {
		return nil, fmt.Errorf("network partition between %s and %s", args.LeaderID, peer)
	}

	m.mu.RLock()
	target, ok := m.nodes[peer]
	m.mu.RUnlock()

	if !ok || target == nil {
		return nil, fmt.Errorf("node %s not found", peer)
	}

	var reply AppendEntriesReply
	err := target.AppendEntries(args, &reply)
	return &reply, err
}

func (m *MockNetworkTransport) InstallSnapshot(ctx context.Context, peer string, args *InstallSnapshotArgs) (*InstallSnapshotReply, error) {
	var reply InstallSnapshotReply
	return &reply, nil
}

func TestRaft_SingleNodeElectionAndProposal(t *testing.T) {
	applyCh := make(chan ApplyMsg, 100)
	cfg := DefaultRaftConfig("node-solo", nil)
	net := NewMockNetworkTransport()

	rn := NewRaftNode(cfg, net, applyCh)
	defer rn.Close()

	becameLeader := false
	for i := 0; i < 60; i++ {
		if rn.IsLeader() {
			becameLeader = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !becameLeader {
		t.Fatalf("expected single node to become leader")
	}

	// Propose document
	docPayload := IndexDocPayload{
		URL:       "https://go.dev/doc/concurrency",
		Title:     "Go Concurrency",
		CleanBody: "Goroutines and channels enable scalable concurrent programming.",
	}
	payloadBytes, _ := json.Marshal(docPayload)

	idx, term, err := rn.Propose(context.Background(), CmdIndexDocument, payloadBytes)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}
	if idx != 1 || term != 1 {
		t.Errorf("expected idx 1, term 1, got idx %d, term %d", idx, term)
	}

	select {
	case msg := <-applyCh:
		if !msg.CommandValid || msg.CommandIndex != 1 {
			t.Fatalf("unexpected apply msg: %+v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for applyCh message")
	}
}

func TestRaft_ThreeNodeClusterElectionAndReplication(t *testing.T) {
	peers := []string{"node-1", "node-2", "node-3"}
	net := NewMockNetworkTransport()

	applyChs := make(map[string]chan ApplyMsg)
	nodes := make(map[string]*RaftNode)
	stateMachines := make(map[string]*StateMachine)

	for _, p := range peers {
		applyCh := make(chan ApplyMsg, 100)
		applyChs[p] = applyCh

		engine := index.NewIndexEngine()
		sm := NewStateMachine(engine, applyCh)
		stateMachines[p] = sm

		cfg := DefaultRaftConfig(p, peers)
		cfg.MinElectionTimeout = 100 * time.Millisecond
		cfg.MaxElectionTimeout = 200 * time.Millisecond
		cfg.HeartbeatInterval = 30 * time.Millisecond

		rn := NewRaftNode(cfg, net, applyCh)
		nodes[p] = rn
		net.Register(p, rn)
	}

	defer func() {
		for _, sm := range stateMachines {
			sm.Stop()
		}
		for _, rn := range nodes {
			rn.Close()
		}
	}()

	// Wait for leader election with polling
	var leader *RaftNode
	var leaderID string
	for i := 0; i < 80; i++ {
		for id, rn := range nodes {
			if rn.IsLeader() {
				leader = rn
				leaderID = id
				break
			}
		}
		if leader != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if leader == nil {
		t.Fatalf("expected 3-node cluster to elect a leader within timeout")
	}
	t.Logf("Leader elected: %s", leaderID)

	// Propose document through leader
	payload := IndexDocPayload{
		URL:         "https://example.com/raft-cluster-test",
		Title:       "Distributed Raft Cluster",
		CleanBody:   "WebLimbAI distributed consensus replication test body.",
		TotalTokens: 8,
		SourceURL:   "https://example.com/raft-cluster-test",
	}
	payloadBytes, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	idx, _, err := leader.Propose(ctx, CmdIndexDocument, payloadBytes)
	if err != nil {
		t.Fatalf("Leader Propose failed: %v", err)
	}
	if idx != 1 {
		t.Errorf("expected log index 1, got %d", idx)
	}

	// Verify all state machines applied the document via polling
	for id, sm := range stateMachines {
		var lastIdx uint64
		for i := 0; i < 60; i++ {
			lastIdx, _ = sm.LastApplied()
			if lastIdx == 1 {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if lastIdx != 1 {
			t.Errorf("node %s did not apply log index 1 (lastApplied=%d)", id, lastIdx)
		}
	}
}

func TestRaft_PartitionAndReelection(t *testing.T) {
	peers := []string{"node-A", "node-B", "node-C"}
	net := NewMockNetworkTransport()

	applyChs := make(map[string]chan ApplyMsg)
	nodes := make(map[string]*RaftNode)

	for _, p := range peers {
		applyCh := make(chan ApplyMsg, 100)
		applyChs[p] = applyCh

		cfg := DefaultRaftConfig(p, peers)
		cfg.MinElectionTimeout = 100 * time.Millisecond
		cfg.MaxElectionTimeout = 200 * time.Millisecond
		cfg.HeartbeatInterval = 30 * time.Millisecond

		rn := NewRaftNode(cfg, net, applyCh)
		nodes[p] = rn
		net.Register(p, rn)
	}

	defer func() {
		for _, rn := range nodes {
			rn.Close()
		}
	}()

	// Find initial leader
	var leaderID string
	for i := 0; i < 80; i++ {
		for id, rn := range nodes {
			if rn.IsLeader() {
				leaderID = id
				break
			}
		}
		if leaderID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if leaderID == "" {
		t.Fatalf("no initial leader elected")
	}

	// Isolate the leader
	net.Partition(leaderID, true)

	// Wait for remaining 2 nodes to elect a new leader
	var newLeaderID string
	for i := 0; i < 80; i++ {
		for id, rn := range nodes {
			if id != leaderID && rn.IsLeader() {
				newLeaderID = id
				break
			}
		}
		if newLeaderID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if newLeaderID == "" {
		t.Fatalf("majority partition failed to elect new leader after leader %s was isolated", leaderID)
	}
	t.Logf("New leader in majority partition: %s", newLeaderID)

	// Reconnect old leader
	net.Partition(leaderID, false)

	// Verify old leader steps down with polling
	oldLeaderSteppedDown := false
	for i := 0; i < 60; i++ {
		if !nodes[leaderID].IsLeader() {
			oldLeaderSteppedDown = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !oldLeaderSteppedDown && newLeaderID != leaderID {
		t.Errorf("old leader %s should have stepped down after reconnecting to higher term leader %s", leaderID, newLeaderID)
	}
}

func TestRaft_UpToDateLogCheck(t *testing.T) {
	applyCh := make(chan ApplyMsg, 10)
	cfg := DefaultRaftConfig("node-test", nil)
	net := NewMockNetworkTransport()

	rn := NewRaftNode(cfg, net, applyCh)
	defer rn.Close()

	rn.mu.Lock()
	rn.currentTerm = 2
	rn.log = []RaftLogEntry{
		{Index: 0, Term: 0},
		{Index: 1, Term: 1},
		{Index: 2, Term: 2},
	}
	rn.mu.Unlock()

	// 1. Candidate with lower term: must reject
	argsStaleTerm := &RequestVoteArgs{
		Term:         1,
		CandidateID:  "candidate-stale",
		LastLogIndex: 10,
		LastLogTerm:  1,
	}
	var replyStaleTerm RequestVoteReply
	_ = rn.RequestVote(argsStaleTerm, &replyStaleTerm)
	if replyStaleTerm.VoteGranted {
		t.Fatalf("granted vote to candidate with lower term")
	}

	// 2. Candidate with same term but shorter log: must reject (Ongaro §5.4.1)
	argsShorterLog := &RequestVoteArgs{
		Term:         2,
		CandidateID:  "candidate-short",
		LastLogIndex: 1,
		LastLogTerm:  2,
	}
	var replyShorterLog RequestVoteReply
	_ = rn.RequestVote(argsShorterLog, &replyShorterLog)
	if replyShorterLog.VoteGranted {
		t.Fatalf("granted vote to candidate with shorter log in same term")
	}

	// 3. Candidate with higher log term: must grant
	argsHigherTermLog := &RequestVoteArgs{
		Term:         3,
		CandidateID:  "candidate-fresh",
		LastLogIndex: 2,
		LastLogTerm:  3,
	}
	var replyHigherTermLog RequestVoteReply
	_ = rn.RequestVote(argsHigherTermLog, &replyHigherTermLog)
	if !replyHigherTermLog.VoteGranted {
		t.Fatalf("expected vote granted to candidate with higher term log")
	}
}
