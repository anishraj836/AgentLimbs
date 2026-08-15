package kafka

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestCheckKafkaReadiness_Unset(t *testing.T) {
	os.Unsetenv("KAFKA_BROKERS")
	ctx := context.Background()
	if err := CheckKafkaReadiness(ctx); err != nil {
		t.Errorf("expected nil error when KAFKA_BROKERS is unset, got %v", err)
	}
}

func TestCheckKafkaReadiness_Valid(t *testing.T) {
	// Start a mock TCP listener
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer l.Close()

	os.Setenv("KAFKA_BROKERS", l.Addr().String())
	defer os.Unsetenv("KAFKA_BROKERS")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := CheckKafkaReadiness(ctx); err != nil {
		t.Errorf("expected successful check to listener, got %v", err)
	}
}

func TestCheckKafkaReadiness_Unreachable(t *testing.T) {
	// Pick an unreachable address
	os.Setenv("KAFKA_BROKERS", "127.0.0.1:54321")
	defer os.Unsetenv("KAFKA_BROKERS")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if err := CheckKafkaReadiness(ctx); err == nil {
		t.Errorf("expected error for unreachable broker, got nil")
	}
}
