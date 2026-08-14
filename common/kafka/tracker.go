package kafka

import (
	"context"
	"sync"

	skafka "github.com/segmentio/kafka-go"
)

type OffsetTracker struct {
	mu        sync.Mutex
	inFlight  map[int]map[int64]bool
	completed map[int]map[int64]skafka.Message
}

func NewOffsetTracker() *OffsetTracker {
	return &OffsetTracker{
		inFlight:  make(map[int]map[int64]bool),
		completed: make(map[int]map[int64]skafka.Message),
	}
}

// MarkStarted registers a message as in-flight before processing begins.
// This allows the tracker to know the exact contiguous sequence of offsets fetched.
func (ot *OffsetTracker) MarkStarted(msg skafka.Message) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	p := msg.Partition
	if ot.inFlight[p] == nil {
		ot.inFlight[p] = make(map[int64]bool)
	}
	ot.inFlight[p][msg.Offset] = true
}

// MarkFailed removes in-flight and completed offsets for a failed message so it does not trap contiguous commits.
func (ot *OffsetTracker) MarkFailed(msg skafka.Message) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	p := msg.Partition
	if ot.inFlight[p] != nil {
		delete(ot.inFlight[p], msg.Offset)
	}
	if ot.completed[p] != nil {
		delete(ot.completed[p], msg.Offset)
	}
}

// RevokePartition removes all in-flight and completed offsets for a revoked partition.
func (ot *OffsetTracker) RevokePartition(partition int) {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	delete(ot.inFlight, partition)
	delete(ot.completed, partition)
}

type Committer interface {
	Commit(ctx context.Context, msg skafka.Message) error
}

// MarkCompleted buffers completed offset messages per partition and commits
// only contiguous, sequential completed message chains to Kafka.
// This prevents out-of-order commits from advancing partition offsets past incomplete messages.
// Offsets are only evicted from completed buffer after commit succeeds, allowing retries on error.
func (ot *OffsetTracker) MarkCompleted(ctx context.Context, consumer Committer, msg skafka.Message) error {
	ot.mu.Lock()

	p := msg.Partition
	if ot.completed[p] == nil {
		ot.completed[p] = make(map[int64]skafka.Message)
	}
	if ot.inFlight[p] == nil {
		ot.inFlight[p] = make(map[int64]bool)
	}

	ot.completed[p][msg.Offset] = msg
	delete(ot.inFlight[p], msg.Offset)

	minInFlight := int64(-1)
	for offset := range ot.inFlight[p] {
		if minInFlight == -1 || offset < minInFlight {
			minInFlight = offset
		}
	}

	var toCommit *skafka.Message
	maxCompleted := int64(-1)

	for offset, m := range ot.completed[p] {
		if minInFlight == -1 || offset < minInFlight {
			if maxCompleted == -1 || offset > maxCompleted {
				maxCompleted = offset
				msgCopy := m
				toCommit = &msgCopy
			}
		}
	}

	ot.mu.Unlock()

	if toCommit != nil {
		err := consumer.Commit(ctx, *toCommit)
		if err != nil {
			return err
		}

		ot.mu.Lock()
		if ot.completed[p] != nil {
			for offset := range ot.completed[p] {
				if offset <= maxCompleted {
					delete(ot.completed[p], offset)
				}
			}
		}
		ot.mu.Unlock()
	}
	return nil
}
