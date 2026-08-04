package redis

import (
	"context"
	"fmt"
)

// BloomFilter is a simple interface over Redis to check if a URL has been seen.
// Note: In production, RedisBloom module would be used (`BF.ADD`, `BF.EXISTS`).
// Here we mock it using a Redis SET if RedisBloom is not installed, but we assume
// a standard set for simplicity if RedisBloom is not available in the alpine image.
type BloomFilter struct {
	key string
}

func NewBloomFilter(key string) *BloomFilter {
	return &BloomFilter{key: key}
}

// Add adds an item to the bloom filter. Returns true if it was newly added.
func (b *BloomFilter) Add(ctx context.Context, item string) (bool, error) {
	if Client == nil {
		return false, fmt.Errorf("redis client is uninitialized")
	}
	// Using standard SET for simplicity. For a real Bloom Filter, 
	// use redis.NewScript or call "BF.ADD" if RedisBloom is loaded.
	res, err := Client.SAdd(ctx, b.key, item).Result()
	if err != nil {
		return false, err
	}
	return res > 0, nil // res > 0 means it was added
}

// Exists checks if an item might exist in the filter
func (b *BloomFilter) Exists(ctx context.Context, item string) (bool, error) {
	if Client == nil {
		return false, fmt.Errorf("redis client is uninitialized")
	}
	return Client.SIsMember(ctx, b.key, item).Result()
}
