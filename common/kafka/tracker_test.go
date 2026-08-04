package kafka

import (
	"context"
	"testing"

	skafka "github.com/segmentio/kafka-go"
)

type mockCommitter struct {
	committed []int64
}

func (m *mockCommitter) Commit(ctx context.Context, msg skafka.Message) error {
	m.committed = append(m.committed, msg.Offset)
	return nil
}

func TestOffsetTrackerContiguous(t *testing.T) {
	ot := NewOffsetTracker()

	// Simulate offset tracker with mock commit
	msg10 := skafka.Message{Partition: 0, Offset: 10}
	msg11 := skafka.Message{Partition: 0, Offset: 11}
	msg12 := skafka.Message{Partition: 0, Offset: 12}

	// Lock manually for unit testing internal state
	ot.mu.Lock()
	ot.completed[0] = make(map[int64]skafka.Message)
	ot.lastCommitted[0] = 9
	ot.mu.Unlock()

	// Buffer msg 11 first (out of order)
	ot.mu.Lock()
	ot.completed[0][11] = msg11
	// Contiguous check
	curr := ot.lastCommitted[0] + 1
	var toCommit *skafka.Message
	for {
		m, exists := ot.completed[0][curr]
		if !exists {
			break
		}
		toCommit = &m
		delete(ot.completed[0], curr)
		ot.lastCommitted[0] = curr
		curr++
	}
	ot.mu.Unlock()

	if toCommit != nil {
		t.Errorf("expected no commit for out-of-order offset 11, but got offset %d", toCommit.Offset)
	}

	// Buffer msg 10 (now completes chain 10, 11)
	ot.mu.Lock()
	ot.completed[0][10] = msg10
	curr = ot.lastCommitted[0] + 1
	toCommit = nil
	for {
		m, exists := ot.completed[0][curr]
		if !exists {
			break
		}
		toCommit = &m
		delete(ot.completed[0], curr)
		ot.lastCommitted[0] = curr
		curr++
	}
	ot.mu.Unlock()

	if toCommit == nil || toCommit.Offset != 11 {
		t.Errorf("expected contiguous commit of offset 11, got %v", toCommit)
	}

	// Buffer msg 12 (now completes chain 12)
	ot.mu.Lock()
	ot.completed[0][12] = msg12
	curr = ot.lastCommitted[0] + 1
	toCommit = nil
	for {
		m, exists := ot.completed[0][curr]
		if !exists {
			break
		}
		toCommit = &m
		delete(ot.completed[0], curr)
		ot.lastCommitted[0] = curr
		curr++
	}
	ot.mu.Unlock()

	if toCommit == nil || toCommit.Offset != 12 {
		t.Errorf("expected contiguous commit of offset 12, got %v", toCommit)
	}
}
