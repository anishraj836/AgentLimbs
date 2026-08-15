package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/crawler-monorepo/internal/search"
	"github.com/go-chi/chi/v5"
)

// ConsensusHTTPTransport configures high-throughput, low-latency connection pooling for consensus RPCs.
var ConsensusHTTPTransport = &http.Transport{
	MaxIdleConns:        200,
	MaxIdleConnsPerHost: 50,
	IdleConnTimeout:     90 * time.Second,
	DisableKeepAlives:   false,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}).DialContext,
}

// SearchHTTPTransport configures scatter-gather query connection pooling with standard deadlines.
var SearchHTTPTransport = &http.Transport{
	MaxIdleConns:        100,
	MaxIdleConnsPerHost: 20,
	IdleConnTimeout:     90 * time.Second,
	DisableKeepAlives:   false,
	DialContext: (&net.Dialer{
		Timeout:   100 * time.Millisecond,
		KeepAlive: 30 * time.Second,
	}).DialContext,
}

// HTTPRaftTransport implements RaftTransport and ShardSearchClient over HTTP JSON-RPC wire.
type HTTPRaftTransport struct {
	consensusClient *http.Client
	searchClient    *http.Client
	peerEndpoints   map[string]string // peerID -> "http://host:port"
}

// NewHTTPRaftTransport initializes an HTTP-based cluster transport.
func NewHTTPRaftTransport(endpoints map[string]string) *HTTPRaftTransport {
	if endpoints == nil {
		endpoints = make(map[string]string)
	}
	return &HTTPRaftTransport{
		consensusClient: &http.Client{
			Transport: ConsensusHTTPTransport,
			Timeout:   40 * time.Millisecond,
		},
		searchClient: &http.Client{
			Transport: SearchHTTPTransport,
			Timeout:   500 * time.Millisecond,
		},
		peerEndpoints: endpoints,
	}
}

// SetPeerEndpoint dynamically associates a node ID with its network URL.
func (t *HTTPRaftTransport) SetPeerEndpoint(peerID, endpoint string) {
	endpoint = strings.TrimRight(endpoint, "/")
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		endpoint = "http://" + endpoint
	}
	t.peerEndpoints[peerID] = endpoint
}

func (t *HTTPRaftTransport) getEndpoint(peer string) (string, error) {
	if ep, ok := t.peerEndpoints[peer]; ok && ep != "" {
		return ep, nil
	}
	// Fallback to peer as endpoint string if valid host:port
	if strings.Contains(peer, ":") || strings.HasPrefix(peer, "http") {
		ep := peer
		if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
			ep = "http://" + ep
		}
		return strings.TrimRight(ep, "/"), nil
	}
	return "", fmt.Errorf("unknown cluster peer endpoint for node %q", peer)
}

func (t *HTTPRaftTransport) RequestVote(ctx context.Context, peer string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	endpoint, err := t.getEndpoint(peer)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/raft/request-vote", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.consensusClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request vote failed with HTTP %d", resp.StatusCode)
	}

	var reply RequestVoteReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (t *HTTPRaftTransport) AppendEntries(ctx context.Context, peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	endpoint, err := t.getEndpoint(peer)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/raft/append-entries", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.consensusClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("append entries failed with HTTP %d", resp.StatusCode)
	}

	var reply AppendEntriesReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

func (t *HTTPRaftTransport) InstallSnapshot(ctx context.Context, peer string, args *InstallSnapshotArgs) (*InstallSnapshotReply, error) {
	endpoint, err := t.getEndpoint(peer)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/raft/install-snapshot", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.consensusClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var reply InstallSnapshotReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// SearchShard executes a remote shard hybrid search query.
func (t *HTTPRaftTransport) SearchShard(ctx context.Context, peer string, query string, topK int) ([]search.HybridSearchHit, error) {
	endpoint, err := t.getEndpoint(peer)
	if err != nil {
		return nil, err
	}

	reqPayload := SearchShardRequest{
		Query: query,
		TopK:  topK,
	}

	body, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/cluster/search", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.searchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("shard search returned HTTP %d", resp.StatusCode)
	}

	var shardResp SearchShardResponse
	if err := json.NewDecoder(resp.Body).Decode(&shardResp); err != nil {
		return nil, err
	}
	return shardResp.Hits, nil
}

// RegisterClusterHTTPHandlers mounts Raft and Shard HTTP routes onto a Chi router.
func RegisterClusterHTTPHandlers(r chi.Router, raftNode *RaftNode, coord *ClusterCoordinator) {
	if raftNode != nil {
		r.Post("/raft/request-vote", func(w http.ResponseWriter, req *http.Request) {
			var args RequestVoteArgs
			if err := json.NewDecoder(io.LimitReader(req.Body, 1024*1024)).Decode(&args); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var reply RequestVoteReply
			_ = raftNode.RequestVote(&args, &reply)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reply)
		})

		r.Post("/raft/append-entries", func(w http.ResponseWriter, req *http.Request) {
			var args AppendEntriesArgs
			if err := json.NewDecoder(io.LimitReader(req.Body, 10*1024*1024)).Decode(&args); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var reply AppendEntriesReply
			_ = raftNode.AppendEntries(&args, &reply)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reply)
		})
	}

	if coord != nil {
		r.Post("/cluster/search", func(w http.ResponseWriter, req *http.Request) {
			var searchReq SearchShardRequest
			if err := json.NewDecoder(io.LimitReader(req.Body, 1024*1024)).Decode(&searchReq); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			hits, err := coord.ExecuteLocalShardSearch(searchReq.Query, searchReq.TopK)
			resp := SearchShardResponse{
				ShardID: coord.NodeID(),
				Hits:    hits,
			}
			if err != nil {
				resp.Error = err.Error()
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		})
	}
}
