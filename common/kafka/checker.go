package kafka

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
)

// CheckKafkaReadiness verifies TCP reachability for all brokers configured in KAFKA_BROKERS.
// If KAFKA_BROKERS is empty, it returns nil (Kafka is not required/configured).
func CheckKafkaReadiness(ctx context.Context) error {
	brokersEnv := os.Getenv("KAFKA_BROKERS")
	if brokersEnv == "" {
		return nil
	}

	brokers := strings.Split(brokersEnv, ",")
	for _, b := range brokers {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, "tcp", b)
		if err != nil {
			return fmt.Errorf("broker %s unreachable: %w", b, err)
		}
		_ = conn.Close()
	}
	return nil
}
