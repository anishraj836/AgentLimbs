package kafka

import (
	"context"
	"sync"

	skafka "github.com/segmentio/kafka-go"
)

type OffsetTracker struct {
	mu            sync.Mutex
	completed     map[int]map[int64]skafka.Message
	lastCommitted map[int]int64
}

func NewOffsetTracker() *OffsetTracker {
	return &OffsetTracker{
		completed:     make(map[int]map[int64]skafka.Message),
		lastCommitted: make(map[int]int64),
	}
}

// MarkCompleted buffers completed offset messages per partition and commits
// only contiguous, sequential completed message chains to Kafka.
// This prevents out-of-order commits from advancing partition offsets past incomplete messages.
func (ot *OffsetTracker) MarkCompleted(ctx context.Context, consumer *Consumer, msg skafka.Message) error {
	ot.mu.Lock()
	defer ot.mu.Unlock()

	p := msg.Partition
	if ot.completed[p] == nil {
		ot.completed[p] = make(map[int64]skafka.Message)
		ot.lastCommitted[p] = msg.Offset - 1
	}

	ot.completed[p][msg.Offset] = msg

	curr := ot.lastCommitted[p] + 1
	var toCommit *skafka.Message

	for {
		m, exists := ot.completed[p][curr]
		if !exists {
			break
		}
		toCommit = &m
		delete(ot.completed[p], curr)
		ot.lastCommitted[p] = curr
		curr++
	}

	if toCommit != nil {
		return consumer.Commit(ctx, *toCommit)
	}
	return nil
}
