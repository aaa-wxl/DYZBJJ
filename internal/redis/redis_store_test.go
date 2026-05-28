package redis

import (
	"context"
	"fmt"
	"os"
	"sync"
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

func TestRedisStorePlaceBidUpdatesSnapshotLeaderboardAndRank(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-redis-1",
		UserID:    "user-a",
		UserName:  "用户A",
		RequestID: "req-a-1",
		Amount:    100,
		Now:       now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("place bid: %v", err)
	}

	if result.Snapshot.CurrentPrice != 100 {
		t.Fatalf("CurrentPrice = %d", result.Snapshot.CurrentPrice)
	}
	if result.Snapshot.HighestBidder != "user-a" {
		t.Fatalf("HighestBidder = %q", result.Snapshot.HighestBidder)
	}
	if result.Snapshot.Rank != 1 {
		t.Fatalf("Rank = %d", result.Snapshot.Rank)
	}
	if len(result.Snapshot.Leaderboard) != 1 || result.Snapshot.Leaderboard[0].Amount != 100 {
		t.Fatalf("Leaderboard = %+v", result.Snapshot.Leaderboard)
	}
}

func TestRedisStorePlaceBidIsIdempotent(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}

	first, err := store.PlaceBid(BidCommand{AuctionID: "auction-redis-1", UserID: "user-a", RequestID: "same-req", Amount: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PlaceBid(BidCommand{AuctionID: "auction-redis-1", UserID: "user-a", RequestID: "same-req", Amount: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}

	if !second.Idempotent {
		t.Fatal("second bid should be idempotent")
	}
	if first.BidID != second.BidID {
		t.Fatalf("BidID changed: %s vs %s", first.BidID, second.BidID)
	}
}

func TestRedisStoreConcurrentBidsNeverDecreasePrice(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.PlaceBid(BidCommand{
				AuctionID: "auction-redis-1",
				UserID:    fmt.Sprintf("user-%02d", i),
				RequestID: fmt.Sprintf("req-%02d", i),
				Amount:    int64(i * 100),
				Now:       now.Add(time.Duration(i) * time.Millisecond),
			})
		}()
	}
	wg.Wait()

	snapshot, err := store.Snapshot("auction-redis-1", "user-20")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentPrice != 2000 {
		t.Fatalf("CurrentPrice = %d, want 2000", snapshot.CurrentPrice)
	}
	if snapshot.Rank != 1 {
		t.Fatalf("Rank = %d, want 1", snapshot.Rank)
	}
}

func TestRedisStoreCeilingPriceWinsOverExtension(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	snapshot := startedSnapshot(now)
	snapshot.EndsAt = now.Add(5 * time.Second)
	snapshot.Rules.CeilingPrice = 1000
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-redis-1",
		UserID:    "user-a",
		RequestID: "req-ceiling",
		Amount:    1000,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Snapshot.Status != auction.StatusSold {
		t.Fatalf("Status = %s", result.Snapshot.Status)
	}
	if result.Extended {
		t.Fatal("ceiling bid should not extend auction")
	}
}
