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
	consumer := &mockCommitter{}

	// Simulate offset tracker with mock commit
	msg10 := skafka.Message{Partition: 0, Offset: 10}
	msg11 := skafka.Message{Partition: 0, Offset: 11}
	msg12 := skafka.Message{Partition: 0, Offset: 12}

	// Mark all as started
	ot.MarkStarted(msg10)
	ot.MarkStarted(msg11)
	ot.MarkStarted(msg12)

	// Buffer msg 12 first (out of order)
	err := ot.MarkCompleted(context.Background(), consumer, msg12)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(consumer.committed) != 0 {
		t.Errorf("expected no commit for out-of-order offset 12, but got %v", consumer.committed)
	}

	// Buffer msg 10
	err = ot.MarkCompleted(context.Background(), consumer, msg10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(consumer.committed) != 1 || consumer.committed[0] != 10 {
		t.Errorf("expected commit of offset 10, got %v", consumer.committed)
	}

	// Buffer msg 11 (now completes chain 10, 11, 12)
	err = ot.MarkCompleted(context.Background(), consumer, msg11)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// We expect the highest contiguous commit, which is 12
	if len(consumer.committed) != 2 || consumer.committed[1] != 12 {
		t.Errorf("expected commit of offset 12, got %v", consumer.committed)
	}
}
