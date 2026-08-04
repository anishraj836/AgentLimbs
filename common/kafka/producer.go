package kafka

import (
	"context"
	"github.com/segmentio/kafka-go"
	"time"
)

type Producer struct {
	writer *kafka.Writer
}

// NewProducer initializes a new Kafka producer
func NewProducer(brokers []string, topic string) *Producer {
	w := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireOne,
		AllowAutoTopicCreation: true,
		BatchTimeout:           10 * time.Millisecond,
	}
	return &Producer{writer: w}
}

// Publish sends a message to Kafka. Key is used for partitioning.
func (p *Producer) Publish(ctx context.Context, key []byte, value []byte) error {
	msg := kafka.Message{
		Key:   key,
		Value: value,
		Time:  time.Now(),
	}
	return p.writer.WriteMessages(ctx, msg)
}

// Close closes the producer
func (p *Producer) Close() error {
	return p.writer.Close()
}
