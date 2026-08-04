package kafka

import (
	"context"
	"github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader *kafka.Reader
}

// NewConsumer initializes a new Kafka consumer in a consumer group
func NewConsumer(brokers []string, groupID string, topic string) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		GroupID:  groupID,
		Topic:    topic,
		MaxBytes: 10e6, // 10MB
	})
	return &Consumer{reader: r}
}

// ReadMessage reads a single message. The consumer must call commit on the message if successful.
func (c *Consumer) ReadMessage(ctx context.Context) (kafka.Message, error) {
	return c.reader.FetchMessage(ctx)
}

// Commit commits the offset of the processed message.
func (c *Consumer) Commit(ctx context.Context, msg kafka.Message) error {
	return c.reader.CommitMessages(ctx, msg)
}

// Close closes the consumer
func (c *Consumer) Close() error {
	return c.reader.Close()
}
