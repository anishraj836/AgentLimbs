package cluster

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/crawler-monorepo/internal/index"
)

var (
	ErrNotLeader      = errors.New("cluster: node is not the Raft leader")
	ErrCommandAborted = errors.New("cluster: raft command was aborted")
)

// IndexDocPayload represents the payload for indexing a document into the cluster.
type IndexDocPayload struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	CleanBody   string `json:"clean_body"`
	TotalTokens int    `json:"total_tokens"`
	SourceURL   string `json:"source_url"`
}

// DeleteDocPayload represents the payload for removing a document from the cluster.
type DeleteDocPayload struct {
	URL string `json:"url"`
}

// SetPrecisionPayload represents the payload for switching vector quantization precision.
type SetPrecisionPayload struct {
	Precision string `json:"precision"`
}

// ApplyMsg is delivered over applyCh whenever a log entry or snapshot is committed.
type ApplyMsg struct {
	CommandValid bool
	Command      RaftLogEntry
	CommandIndex uint64
	Snapshot     []byte
}

// StateMachine manages the sequential execution of committed Raft commands onto the index.Engine.
type StateMachine struct {
	engine   *index.Engine
	applyCh  chan ApplyMsg
	stopCh   chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex
	lastIdx  uint64
	lastTerm uint64
}

// NewStateMachine creates a new StateMachine bound to an index.Engine.
func NewStateMachine(engine *index.Engine, applyCh chan ApplyMsg) *StateMachine {
	if engine == nil {
		engine = index.GlobalEngine
	}
	sm := &StateMachine{
		engine:  engine,
		applyCh: applyCh,
		stopCh:  make(chan struct{}),
	}
	sm.wg.Add(1)
	go sm.applyLoop()
	return sm
}

// applyLoop consumes committed ApplyMsg items sequentially to ensure deterministic state machine transitions.
func (sm *StateMachine) applyLoop() {
	defer sm.wg.Done()

	for {
		select {
		case <-sm.stopCh:
			return
		case msg, ok := <-sm.applyCh:
			if !ok {
				return
			}
			if msg.CommandValid {
				sm.applyCommand(msg.Command)
				sm.mu.Lock()
				sm.lastIdx = msg.CommandIndex
				sm.lastTerm = msg.Command.Term
				sm.mu.Unlock()
			} else if len(msg.Snapshot) > 0 {
				sm.applySnapshot(msg.Snapshot)
			}
		}
	}
}

func (sm *StateMachine) applyCommand(entry RaftLogEntry) {
	switch entry.Type {
	case CmdIndexDocument:
		var p IndexDocPayload
		if err := json.Unmarshal(entry.Payload, &p); err == nil && p.URL != "" {
			sm.engine.IndexDocumentDirectly(
				p.URL,
				p.Title,
				p.CleanBody,
				p.TotalTokens,
				p.SourceURL,
			)
		}

	case CmdDeleteDocument:
		var p DeleteDocPayload
		if err := json.Unmarshal(entry.Payload, &p); err == nil && p.URL != "" {
			sm.engine.DeleteDocument(p.URL)
		}

	case CmdSetPrecision:
		var p SetPrecisionPayload
		if err := json.Unmarshal(entry.Payload, &p); err == nil && p.Precision != "" {
			if vecIdx := sm.engine.GetVectorIndex(); vecIdx != nil {
				_ = vecIdx.SetPrecision(index.VectorPrecision(p.Precision))
			}
		}
	}
}

func (sm *StateMachine) applySnapshot(data []byte) {
	// In-memory snapshot reload if needed
	if len(data) == 0 {
		return
	}
}

// Stop gracefully terminates the state machine apply loop.
func (sm *StateMachine) Stop() {
	close(sm.stopCh)
	sm.wg.Wait()
}

// LastApplied returns the last applied log index and term.
func (sm *StateMachine) LastApplied() (uint64, uint64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.lastIdx, sm.lastTerm
}
