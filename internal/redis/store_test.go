// redis 测试覆盖出价原子性、幂等性和封顶成交优先级。
package redis

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

func TestStorePlaceBidRejectsLowBid(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(100, 0)
	snapshot := auction.Snapshot{
		AuctionID:    "auction-1",
		Status:       auction.StatusRunning,
		CurrentPrice: 100,
		EndsAt:       now.Add(time.Minute),
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       50,
			Duration:        time.Minute,
			CeilingPrice:    1000,
			ExtendThreshold: 10 * time.Second,
			ExtendBy:        20 * time.Second,
		},
	}
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatalf("init auction: %v", err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-1",
		UserID:    "user-1",
		RequestID: "req-1",
		Amount:    120,
		Now:       now,
	})

	if err == nil {
		t.Fatal("expected low bid to be rejected")
	}
	if result.NextMinimum != 150 {
		t.Fatalf("next minimum = %d, want 150", result.NextMinimum)
	}
}

func TestStoreSnapshotIncludesTopFiveLeaderboardAndRank(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:      "auction-1",
		Status:         auction.StatusRunning,
		CurrentPrice:   0,
		EndsAt:         now.Add(time.Minute),
		ServerTime:     now,
		NextMinimumBid: 100,
		Rules: auction.Rules{
			StartPrice:   0,
			Increment:    100,
			Duration:     time.Minute,
			CeilingPrice: 2000,
		},
	}); err != nil {
		t.Fatal(err)
	}
	for i, bid := range []struct {
		user   string
		amount int64
	}{
		{"usr-user-a", 100},
		{"usr-user-b", 200},
		{"usr-user-c", 300},
		{"usr-user-d", 400},
		{"usr-user-e", 500},
		{"usr-user-f", 600},
	} {
		_, err := store.PlaceBid(BidCommand{
			AuctionID: "auction-1",
			UserID:    bid.user,
			RequestID: fmt.Sprintf("req-%d", i),
			Amount:    bid.amount,
			Now:       now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("PlaceBid(%s) error = %v", bid.user, err)
		}
	}
	snapshot, err := store.Snapshot("auction-1", "usr-user-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Leaderboard) != 5 {
		t.Fatalf("leaderboard len = %d, want 5: %+v", len(snapshot.Leaderboard), snapshot.Leaderboard)
	}
	if snapshot.Leaderboard[0].UserID != "usr-user-f" || snapshot.Leaderboard[0].Amount != 600 {
		t.Fatalf("leaderboard[0] = %+v", snapshot.Leaderboard[0])
	}
	if snapshot.Rank != 6 {
		t.Fatalf("rank = %d, want 6", snapshot.Rank)
	}
}

func TestStoreLeaderboardKeepsEarlierBidFirstOnTie(t *testing.T) {
	snapshot := snapshotWithRank(auction.Snapshot{AuctionID: "auction-1"}, map[string]rankingEntry{
		"usr-user-a": {Amount: 100, Seq: 1},
		"usr-user-b": {Amount: 100, Seq: 2},
	}, "usr-user-b")
	if snapshot.Leaderboard[0].UserID != "usr-user-a" || snapshot.Leaderboard[1].UserID != "usr-user-b" {
		t.Fatalf("leaderboard tie order = %+v", snapshot.Leaderboard)
	}
	if snapshot.Rank != 2 {
		t.Fatalf("rank = %d, want 2", snapshot.Rank)
	}
}

func TestStorePlaceBidIsIdempotent(t *testing.T) {
	store := newStartedStore(t)
	now := time.Unix(100, 0)

	first, err := store.PlaceBid(BidCommand{AuctionID: "auction-1", UserID: "user-1", RequestID: "req-1", Amount: 100, Now: now})
	if err != nil {
		t.Fatalf("first bid: %v", err)
	}
	second, err := store.PlaceBid(BidCommand{AuctionID: "auction-1", UserID: "user-1", RequestID: "req-1", Amount: 100, Now: now})
	if err != nil {
		t.Fatalf("second bid: %v", err)
	}

	if !second.Idempotent {
		t.Fatal("expected second result to be idempotent")
	}
	if first.BidID != second.BidID {
		t.Fatalf("bid id changed across idempotent request: %s vs %s", first.BidID, second.BidID)
	}
}

func TestStoreConcurrentBidsNeverDecreasePrice(t *testing.T) {
	store := newStartedStore(t)
	now := time.Unix(100, 0)
	var wg sync.WaitGroup

	for i := 1; i <= 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.PlaceBid(BidCommand{
				AuctionID: "auction-1",
				UserID:    "user",
				RequestID: "req-concurrent-" + string(rune('a'+i)),
				Amount:    int64(i * 100),
				Now:       now,
			})
		}()
	}
	wg.Wait()

	snapshot, err := store.Snapshot("auction-1", "user")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.CurrentPrice != 2000 {
		t.Fatalf("current price = %d, want 2000", snapshot.CurrentPrice)
	}
}

func TestStoreCeilingPriceWinsOverExtension(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(150, 0)
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:    "auction-1",
		Status:       auction.StatusRunning,
		CurrentPrice: 0,
		EndsAt:       now.Add(5 * time.Second),
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       100,
			Duration:        time.Minute,
			CeilingPrice:    1000,
			ExtendThreshold: 20 * time.Second,
			ExtendBy:        30 * time.Second,
		},
	}); err != nil {
		t.Fatalf("init auction: %v", err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-1",
		UserID:    "user-1",
		RequestID: "req-ceiling",
		Amount:    1000,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("ceiling bid: %v", err)
	}

	if result.Snapshot.Status != auction.StatusSold {
		t.Fatalf("status = %s, want %s", result.Snapshot.Status, auction.StatusSold)
	}
	if result.Extended {
		t.Fatal("ceiling bid must not extend auction")
	}
}

func TestStorePlaceBidExtensionKeepsAuctionRunning(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(150, 0)
	endsAt := now.Add(5 * time.Second)
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:    "auction-1",
		Status:       auction.StatusRunning,
		CurrentPrice: 0,
		EndsAt:       endsAt,
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       100,
			Duration:        time.Minute,
			CeilingPrice:    1000,
			ExtendThreshold: 20 * time.Second,
			ExtendBy:        30 * time.Second,
		},
	}); err != nil {
		t.Fatalf("init auction: %v", err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-1",
		UserID:    "user-1",
		RequestID: "req-extend",
		Amount:    100,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("place bid: %v", err)
	}

	if !result.Extended {
		t.Fatal("expected bid to extend auction")
	}
	if result.Snapshot.Status != auction.StatusRunning {
		t.Fatalf("status = %s, want %s", result.Snapshot.Status, auction.StatusRunning)
	}
	if !result.Snapshot.EndsAt.Equal(endsAt.Add(30 * time.Second)) {
		t.Fatalf("ends at = %s, want %s", result.Snapshot.EndsAt, endsAt.Add(30*time.Second))
	}
}

func TestStoreCancelAllowsDraftAndRunning(t *testing.T) {
	now := time.Unix(200, 0)
	tests := []struct {
		name   string
		status auction.Status
	}{
		{name: "draft", status: auction.StatusDraft},
		{name: "running", status: auction.StatusRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			if err := store.InitAuction(auction.Snapshot{
				AuctionID:    "auction-1",
				Status:       tt.status,
				CurrentPrice: 0,
				EndsAt:       now.Add(time.Minute),
				Rules: auction.Rules{
					StartPrice:      0,
					Increment:       100,
					Duration:        time.Minute,
					CeilingPrice:    1000,
					ExtendThreshold: 20 * time.Second,
					ExtendBy:        30 * time.Second,
				},
			}); err != nil {
				t.Fatalf("init auction: %v", err)
			}

			result, err := store.Cancel("auction-1", now)
			if err != nil {
				t.Fatalf("cancel: %v", err)
			}
			if result.Status != auction.StatusCancelled {
				t.Fatalf("status = %s, want %s", result.Status, auction.StatusCancelled)
			}
		})
	}
}

func TestStoreCancelRejectsTerminalStatuses(t *testing.T) {
	now := time.Unix(200, 0)
	tests := []struct {
		name   string
		status auction.Status
	}{
		{name: "sold", status: auction.StatusSold},
		{name: "ended", status: auction.StatusEnded},
		{name: "cancelled", status: auction.StatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			if err := store.InitAuction(auction.Snapshot{
				AuctionID:     "auction-1",
				Status:        tt.status,
				CurrentPrice:  100,
				HighestBidder: "user-1",
				EndsAt:        now.Add(-time.Second),
				Rules: auction.Rules{
					StartPrice:      0,
					Increment:       100,
					Duration:        time.Minute,
					CeilingPrice:    1000,
					ExtendThreshold: 20 * time.Second,
					ExtendBy:        30 * time.Second,
				},
			}); err != nil {
				t.Fatalf("init auction: %v", err)
			}

			result, err := store.Cancel("auction-1", now)
			if err == nil {
				t.Fatal("expected terminal status to be rejected")
			}
			if result.Status != tt.status {
				t.Fatalf("result status = %s, want %s", result.Status, tt.status)
			}

			snapshot, err := store.Snapshot("auction-1", "")
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if snapshot.Status != tt.status {
				t.Fatalf("stored status = %s, want %s", snapshot.Status, tt.status)
			}
		})
	}
}

func TestStoreFinishExpiredRejectsTerminalStatuses(t *testing.T) {
	now := time.Unix(200, 0)
	tests := []struct {
		name   string
		status auction.Status
	}{
		{name: "sold", status: auction.StatusSold},
		{name: "ended", status: auction.StatusEnded},
		{name: "cancelled", status: auction.StatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			if err := store.InitAuction(auction.Snapshot{
				AuctionID:     "auction-1",
				Status:        tt.status,
				CurrentPrice:  100,
				HighestBidder: "user-1",
				EndsAt:        now.Add(-time.Second),
				Rules: auction.Rules{
					StartPrice:      0,
					Increment:       100,
					Duration:        time.Minute,
					CeilingPrice:    1000,
					ExtendThreshold: 20 * time.Second,
					ExtendBy:        30 * time.Second,
				},
			}); err != nil {
				t.Fatalf("init auction: %v", err)
			}

			result, err := store.FinishExpired("auction-1", now)
			if err == nil {
				t.Fatal("expected terminal status to be rejected")
			}
			if result.Status != tt.status {
				t.Fatalf("result status = %s, want %s", result.Status, tt.status)
			}

			snapshot, err := store.Snapshot("auction-1", "")
			if err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			if snapshot.Status != tt.status {
				t.Fatalf("stored status = %s, want %s", snapshot.Status, tt.status)
			}
		})
	}
}

func newStartedStore(t *testing.T) *MemoryStore {
	t.Helper()
	store := NewMemoryStore()
	now := time.Unix(100, 0)
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:    "auction-1",
		Status:       auction.StatusRunning,
		CurrentPrice: 0,
		EndsAt:       now.Add(time.Minute),
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       100,
			Duration:        time.Minute,
			CeilingPrice:    10000,
			ExtendThreshold: 20 * time.Second,
			ExtendBy:        30 * time.Second,
		},
	}); err != nil {
		t.Fatalf("init auction: %v", err)
	}
	return store
}
