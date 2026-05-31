package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"realtime-auction-core/internal/config"
	rtredis "realtime-auction-core/internal/redis"
)

func TestConfigureRealtimeStoreFallsBackWhenRedisOptional(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	runtime, err := configureRealtimeStore(ctx, config.Config{
		RedisAddr:     "127.0.0.1:0",
		RedisRequired: false,
	})
	if err != nil {
		t.Fatalf("configure realtime store: %v", err)
	}
	defer runtime.Close()

	if _, ok := runtime.Store.(*rtredis.MemoryStore); !ok {
		t.Fatalf("store = %T, want *redis.MemoryStore", runtime.Store)
	}
	if runtime.RedisClient != nil {
		t.Fatal("RedisClient should be nil when falling back to memory store")
	}
}

func TestConfigureRealtimeStoreFailsWhenRedisRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	runtime, err := configureRealtimeStore(ctx, config.Config{
		RedisAddr:     "127.0.0.1:0",
		RedisRequired: true,
	})
	defer runtime.Close()
	if err == nil {
		t.Fatal("expected redis connection error")
	}
	if !errors.Is(err, errRedisUnavailable) {
		t.Fatalf("error = %v, want errRedisUnavailable", err)
	}
	if !strings.Contains(err.Error(), "docker compose up -d redis mysql") {
		t.Fatalf("error = %q, want docker compose hint", err.Error())
	}
}
