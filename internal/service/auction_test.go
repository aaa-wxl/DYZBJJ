package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/ws"
)

func TestAuctionAutomaticallyEndsAfterDuration(t *testing.T) {
	repo, _, svc, a := newAuctionServiceFixture(t, auction.Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        20 * time.Millisecond,
		CeilingPrice:    1000,
		ExtendThreshold: 0,
		ExtendBy:        0,
	})

	if _, err := svc.StartAuction(a.ID, time.Now().UTC()); err != nil {
		t.Fatalf("start auction: %v", err)
	}

	waitForStatus(t, repo, a.ID, auction.StatusEnded, 200*time.Millisecond)
}

func TestAuctionCeilingBidSellsImmediatelyAndCreatesOrder(t *testing.T) {
	repo, _, svc, a := newAuctionServiceFixture(t, auction.Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        time.Minute,
		CeilingPrice:    500,
		ExtendThreshold: 30 * time.Second,
		ExtendBy:        30 * time.Second,
	})

	started, err := svc.StartAuction(a.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("start auction: %v", err)
	}
	result, err := svc.PlaceBid(redis.BidCommand{
		AuctionID: a.ID,
		UserID:    "user-1",
		RequestID: "req-ceiling",
		Amount:    500,
		Now:       started.ServerTime.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("place ceiling bid: %v", err)
	}

	if result.Snapshot.Status != auction.StatusSold {
		t.Fatalf("status = %s, want %s", result.Snapshot.Status, auction.StatusSold)
	}
	order, err := repo.GetOrderByAuction(a.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.BuyerID != "user-1" || order.FinalPrice != 500 {
		t.Fatalf("order = %+v, want buyer user-1 final price 500", order)
	}
}

func TestAuctionExtensionReschedulesAutomaticFinish(t *testing.T) {
	repo, _, svc, a := newAuctionServiceFixture(t, auction.Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        40 * time.Millisecond,
		CeilingPrice:    1000,
		ExtendThreshold: 25 * time.Millisecond,
		ExtendBy:        80 * time.Millisecond,
	})

	started, err := svc.StartAuction(a.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("start auction: %v", err)
	}
	bidAt := started.EndsAt.Add(-20 * time.Millisecond)
	result, err := svc.PlaceBid(redis.BidCommand{
		AuctionID: a.ID,
		UserID:    "user-1",
		RequestID: "req-extend",
		Amount:    100,
		Now:       bidAt,
	})
	if err != nil {
		t.Fatalf("place extending bid: %v", err)
	}
	if !result.Extended {
		t.Fatal("expected bid to extend auction")
	}

	time.Sleep(time.Until(started.EndsAt) + 25*time.Millisecond)
	current, err := repo.GetAuction(a.ID)
	if err != nil {
		t.Fatalf("get auction: %v", err)
	}
	if current.Status != auction.StatusRunning {
		t.Fatalf("status after old end = %s, want %s", current.Status, auction.StatusRunning)
	}

	waitForStatus(t, repo, a.ID, auction.StatusSold, 200*time.Millisecond)
}

func TestAuctionCancelStopsAutomaticFinish(t *testing.T) {
	repo, _, svc, a := newAuctionServiceFixture(t, auction.Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        25 * time.Millisecond,
		CeilingPrice:    1000,
		ExtendThreshold: 0,
		ExtendBy:        0,
	})

	if _, err := svc.StartAuction(a.ID, time.Now().UTC()); err != nil {
		t.Fatalf("start auction: %v", err)
	}
	if _, err := svc.CancelAuction(a.ID, time.Now().UTC()); err != nil {
		t.Fatalf("cancel auction: %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	got, err := repo.GetAuction(a.ID)
	if err != nil {
		t.Fatalf("get auction: %v", err)
	}
	if got.Status != auction.StatusCancelled {
		t.Fatalf("status = %s, want %s", got.Status, auction.StatusCancelled)
	}
}

func TestPlaceBidReturnsAuditSaveError(t *testing.T) {
	baseRepo := repository.NewMemoryRepository()
	repo := &failingBidRepository{MemoryRepository: baseRepo}
	store := redis.NewMemoryStore()
	svc := NewAuctionService(repo, store, ws.NewHub())
	a, err := svc.CreateAuction("merchant-1", auction.Product{Name: "jade"}, auction.Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        time.Minute,
		CeilingPrice:    1000,
		ExtendThreshold: 0,
		ExtendBy:        0,
	})
	if err != nil {
		t.Fatalf("create auction: %v", err)
	}
	started, err := svc.StartAuction(a.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("start auction: %v", err)
	}

	_, err = svc.PlaceBid(redis.BidCommand{
		AuctionID: a.ID,
		UserID:    "user-1",
		RequestID: "req-save-fails",
		Amount:    100,
		Now:       started.ServerTime.Add(time.Second),
	})
	if err == nil {
		t.Fatal("expected save bid audit error")
	}
	if !strings.Contains(err.Error(), "save bid audit") {
		t.Fatalf("error = %v, want save bid audit context", err)
	}
}

func TestAuctionSnapshotEnrichesLeaderboardDisplayNames(t *testing.T) {
	repo := repository.NewMemoryRepository()
	now := time.Now().UTC()
	if err := repo.UpsertUser(auction.User{
		ID:          "usr-user-a",
		Username:    "userA",
		DisplayName: "用户A",
		Role:        auction.RoleBidder,
		Status:      auction.UserActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewAuctionService(repo, redis.NewMemoryStore(), ws.NewHub())
	a, err := svc.CreateAuction("merchant", auction.Product{Name: "Lot"}, auction.Rules{
		StartPrice:   0,
		Increment:    100,
		Duration:     time.Minute,
		CeilingPrice: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := svc.StartAuction(a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.PlaceBid(redis.BidCommand{
		AuctionID: a.ID,
		UserID:    "usr-user-a",
		RequestID: "req-a",
		Amount:    100,
		Now:       started.ServerTime.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Leaderboard) != 1 || result.Snapshot.Leaderboard[0].DisplayName != "用户A" {
		t.Fatalf("leaderboard = %+v", result.Snapshot.Leaderboard)
	}
}

func newAuctionServiceFixture(t *testing.T, rules auction.Rules) (*repository.MemoryRepository, *redis.MemoryStore, *AuctionService, auction.Auction) {
	t.Helper()
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	svc := NewAuctionService(repo, store, ws.NewHub())
	a, err := svc.CreateAuction("merchant-1", auction.Product{Name: "jade"}, rules)
	if err != nil {
		t.Fatalf("create auction: %v", err)
	}
	return repo, store, svc, a
}

func waitForStatus(t *testing.T, repo *repository.MemoryRepository, id string, want auction.Status, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, err := repo.GetAuction(id)
		if err != nil {
			t.Fatalf("get auction: %v", err)
		}
		if got.Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got, err := repo.GetAuction(id)
	if err != nil {
		t.Fatalf("get auction: %v", err)
	}
	t.Fatalf("status = %s, want %s within %s", got.Status, want, timeout)
}

type failingBidRepository struct {
	*repository.MemoryRepository
}

func (r *failingBidRepository) SaveBid(auction.Bid) error {
	return errors.New("disk is full")
}
