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

type failingCommitter struct {
	committed []int64
	failCount int
	calls     int
}

func (m *failingCommitter) Commit(ctx context.Context, msg skafka.Message) error {
	m.calls++
	if m.calls <= m.failCount {
		return context.DeadlineExceeded
	}
	m.committed = append(m.committed, msg.Offset)
	return nil
}

func TestOffsetTrackerCommitErrorRetry(t *testing.T) {
	ot := NewOffsetTracker()
	consumer := &failingCommitter{failCount: 1} // Fails first commit, succeeds second

	msg1 := skafka.Message{Partition: 0, Offset: 1}
	msg2 := skafka.Message{Partition: 0, Offset: 2}

	ot.MarkStarted(msg1)
	ot.MarkStarted(msg2)

	// First commit fails
	err := ot.MarkCompleted(context.Background(), consumer, msg1)
	if err == nil {
		t.Fatalf("expected error from failing committer, got nil")
	}
	if len(consumer.committed) != 0 {
		t.Errorf("expected no successful commit, got %v", consumer.committed)
	}

	// Next completion should retry offset 1 along with offset 2
	err = ot.MarkCompleted(context.Background(), consumer, msg2)
	if err != nil {
		t.Fatalf("expected second commit to succeed, got error: %v", err)
	}
	if len(consumer.committed) != 1 || consumer.committed[0] != 2 {
		t.Errorf("expected commit of offset 2 on retry, got %v", consumer.committed)
	}
}

func TestOffsetTrackerMarkFailed_PreventsCommitPastFailedOffset(t *testing.T) {
	ot := NewOffsetTracker()
	consumer := &mockCommitter{}

	msg0 := skafka.Message{Partition: 0, Offset: 0}
	msg1 := skafka.Message{Partition: 0, Offset: 1}
	msg2 := skafka.Message{Partition: 0, Offset: 2}

	ot.MarkStarted(msg0)
	ot.MarkStarted(msg1)
	ot.MarkStarted(msg2)

	// msg0 completes -> commits offset 0
	err := ot.MarkCompleted(context.Background(), consumer, msg0)
	if err != nil {
		t.Fatalf("unexpected error on msg0 complete: %v", err)
	}
	if len(consumer.committed) != 1 || consumer.committed[0] != 0 {
		t.Fatalf("expected commit of offset 0, got %v", consumer.committed)
	}

	// msg1 fails (e.g. panic or unmarshal error)
	err = ot.MarkFailed(context.Background(), consumer, msg1)
	if err != nil {
		t.Fatalf("unexpected error on msg1 fail: %v", err)
	}

	// msg2 completes; since msg1 failed, committing msg2 would skip msg1 in Kafka.
	// Tracker must block committing past failed offset 1 to guarantee retry on restart.
	err = ot.MarkCompleted(context.Background(), consumer, msg2)
	if err != nil {
		t.Fatalf("unexpected error on msg2 complete: %v", err)
	}
	if len(consumer.committed) != 1 {
		t.Errorf("expected no further commits past failed offset 1 (kept at offset 0), got %v", consumer.committed)
	}
}

func TestOffsetTrackerRevokePartition(t *testing.T) {
	ot := NewOffsetTracker()

	msg1 := skafka.Message{Partition: 3, Offset: 100}
	ot.MarkStarted(msg1)

	ot.RevokePartition(3)

	ot.mu.Lock()
	if ot.inFlight[3] != nil || ot.completed[3] != nil {
		t.Errorf("expected partition 3 to be cleared after revoke")
	}
	ot.mu.Unlock()
}
