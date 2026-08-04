package redis

import (
	"context"
	"github.com/redis/go-redis/v9"
)

var Client *redis.Client

func InitRedis(addr string, password string, db int) {
	if addr == "" {
		addr = "localhost:6379"
	}
	Client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	_, err := Client.Ping(context.Background()).Result()
	if err != nil {
		panic("Failed to connect to Redis: " + err.Error())
	}
}
