package redis

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/domain/auction"
)

func newRedisStoreForTest(t *testing.T) (*RedisStore, *goredis.Client) {
	t.Helper()
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run Redis integration tests")
	}
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return NewRedisStore(client), client
}

func startedSnapshot(now time.Time) auction.Snapshot {
	return auction.Snapshot{
		AuctionID:      "auction-redis-1",
		Product:        auction.Product{Name: "星河翡翠手镯", ImageURL: "/demo.jpg", Description: "demo"},
		Status:         auction.StatusRunning,
		CurrentPrice:   0,
		EndsAt:         now.Add(time.Minute),
		ServerTime:     now,
		NextMinimumBid: 100,
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       100,
			Duration:        time.Minute,
			CeilingPrice:    10_000,
			ExtendThreshold: 20 * time.Second,
			ExtendBy:        30 * time.Second,
		},
	}
}

func TestRedisStoreInitAndSnapshot(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()

	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatalf("init auction: %v", err)
	}
	snapshot, err := store.Snapshot("auction-redis-1", "user-a")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snapshot.AuctionID != "auction-redis-1" {
		t.Fatalf("AuctionID = %q", snapshot.AuctionID)
	}
	if snapshot.Product.Name != "星河翡翠手镯" {
		t.Fatalf("Product.Name = %q", snapshot.Product.Name)
	}
	if snapshot.CurrentPrice != 0 {
		t.Fatalf("CurrentPrice = %d", snapshot.CurrentPrice)
	}
	if snapshot.NextMinimumBid != 100 {
		t.Fatalf("NextMinimumBid = %d", snapshot.NextMinimumBid)
	}
}
