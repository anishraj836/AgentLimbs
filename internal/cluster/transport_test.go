package cluster

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/crawler-monorepo/internal/index"
	"github.com/go-chi/chi/v5"
)

func TestHTTPRaftTransport_RequestVoteAndAppendEntries(t *testing.T) {
	applyCh := make(chan ApplyMsg, 100)
	cfg := DefaultRaftConfig("server-node", nil)
	netMock := NewMockNetworkTransport()
	raftNode := NewRaftNode(cfg, netMock, applyCh)
	defer raftNode.Close()

	ring := NewHashRing(128)
	ring.AddNode("server-node")
	eng := index.NewEngine()
	coord := NewClusterCoordinator("server-node", ring, raftNode, eng, nil, 16)

	r := chi.NewRouter()
	RegisterClusterHTTPHandlers(r, raftNode, coord)

	ts := httptest.NewServer(r)
	defer ts.Close()

	transport := NewHTTPRaftTransport(map[string]string{
		"server-node": ts.URL,
	})

	// 1. Test RequestVote RPC
	voteArgs := &RequestVoteArgs{
		Term:         10,
		CandidateID:  "client-candidate",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}
	voteReply, err := transport.RequestVote(context.Background(), "server-node", voteArgs)
	if err != nil {
		t.Fatalf("RequestVote failed: %v", err)
	}
	if !voteReply.VoteGranted {
		t.Errorf("expected vote granted for term 10, got %v", voteReply.VoteGranted)
	}

	// 2. Test AppendEntries RPC
	appendArgs := &AppendEntriesArgs{
		Term:         10,
		LeaderID:     "client-candidate",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries: []RaftLogEntry{
			{Index: 1, Term: 10, Type: CmdIndexDocument, Payload: []byte(`{"url":"https://go.dev"}`)},
		},
		LeaderCommit: 1,
	}
	appendReply, err := transport.AppendEntries(context.Background(), "server-node", appendArgs)
	if err != nil {
		t.Fatalf("AppendEntries failed: %v", err)
	}
	if !appendReply.Success {
		t.Errorf("expected AppendEntries success, got %v", appendReply.Success)
	}
}

func TestHTTPRaftTransport_SearchShard(t *testing.T) {
	eng := index.NewEngine()
	eng.IndexDocumentDirectly("https://golang.org/doc", "Go Documentation", "Go is an open source programming language.", 8, "https://golang.org/doc")

	ring := NewHashRing(128)
	ring.AddNode("search-shard-1")
	coord := NewClusterCoordinator("search-shard-1", ring, nil, eng, nil, 16)

	r := chi.NewRouter()
	RegisterClusterHTTPHandlers(r, nil, coord)

	ts := httptest.NewServer(r)
	defer ts.Close()

	transport := NewHTTPRaftTransport(map[string]string{
		"search-shard-1": ts.URL,
	})

	hits, err := transport.SearchShard(context.Background(), "search-shard-1", "programming language", 5)
	if err != nil {
		t.Fatalf("SearchShard failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected search hits, got 0")
	}
	if hits[0].Title != "Go Documentation" {
		t.Errorf("expected title 'Go Documentation', got: %s", hits[0].Title)
	}
}
