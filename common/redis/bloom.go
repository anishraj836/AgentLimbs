package redis

import (
	"context"
	"fmt"
)

// SetBasedDedup is a simple interface over Redis to check if a URL has been seen using a Redis SET.
type SetBasedDedup struct {
	key string
}

func NewSetBasedDedup(key string) *SetBasedDedup {
	return &SetBasedDedup{key: key}
}

// Add adds an item to the deduplication set. Returns true if it was newly added.
func (s *SetBasedDedup) Add(ctx context.Context, item string) (bool, error) {
	if Client == nil {
		return false, fmt.Errorf("redis client is uninitialized")
	}
	res, err := Client.SAdd(ctx, s.key, item).Result()
	if err != nil {
		return false, err
	}
	return res > 0, nil // res > 0 means it was added
}

// Exists checks if an item exists in the deduplication set.
func (s *SetBasedDedup) Exists(ctx context.Context, item string) (bool, error) {
	if Client == nil {
		return false, fmt.Errorf("redis client is uninitialized")
	}
	return Client.SIsMember(ctx, s.key, item).Result()
}
