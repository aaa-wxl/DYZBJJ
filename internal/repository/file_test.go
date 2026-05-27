package repository

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

func TestFileRepositoryPersistsRecordsAcrossInstancesAndKeepsFirstOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Add(-time.Hour).Round(0)

	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	user := auction.User{
		ID:          "user-1",
		Username:    "alice",
		DisplayName: "Alice",
		Role:        auction.RoleBidder,
		Status:      auction.UserActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	session := auction.Session{
		Token:     "token-1",
		UserID:    user.ID,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	a := auction.Auction{
		ID:         "auction-1",
		MerchantID: "merchant-1",
		Product: auction.Product{
			Name:        "Camera",
			ImageURL:    "https://example.test/camera.jpg",
			Description: "Mirrorless camera",
		},
		Rules: auction.Rules{
			StartPrice:      100,
			Increment:       10,
			Duration:        time.Hour,
			CeilingPrice:    500,
			ExtendThreshold: 5 * time.Minute,
			ExtendBy:        time.Minute,
		},
		Status:       auction.StatusRunning,
		CurrentPrice: 120,
		StartsAt:     now,
		EndsAt:       now.Add(time.Hour),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	bid := auction.Bid{
		ID:        "bid-1",
		AuctionID: a.ID,
		UserID:    user.ID,
		RequestID: "request-1",
		Amount:    120,
		CreatedAt: now.Add(time.Minute),
	}
	firstOrder := auction.Order{
		ID:          "order-1",
		AuctionID:   a.ID,
		ProductName: a.Product.Name,
		BuyerID:     user.ID,
		FinalPrice:  120,
		Status:      "CREATED",
		CreatedAt:   now.Add(2 * time.Minute),
	}
	secondOrder := auction.Order{
		ID:          "order-2",
		AuctionID:   a.ID,
		ProductName: a.Product.Name,
		BuyerID:     "user-2",
		FinalPrice:  130,
		Status:      "CREATED",
		CreatedAt:   now.Add(3 * time.Minute),
	}

	if err := repo.SaveUser(user); err != nil {
		t.Fatalf("SaveUser() error = %v", err)
	}
	if err := repo.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	if _, err := repo.CreateAuction(a); err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}
	if err := repo.SaveBid(bid); err != nil {
		t.Fatalf("SaveBid() error = %v", err)
	}
	if got, err := repo.UpsertOrder(firstOrder); err != nil || got != firstOrder {
		t.Fatalf("UpsertOrder(first) = (%+v, %v), want (%+v, nil)", got, err, firstOrder)
	}
	if got, err := repo.UpsertOrder(secondOrder); err != nil || got != firstOrder {
		t.Fatalf("UpsertOrder(second) = (%+v, %v), want (%+v, nil)", got, err, firstOrder)
	}

	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository(reloaded) error = %v", err)
	}

	gotUser, err := reloaded.GetUserByToken(session.Token)
	if err != nil {
		t.Fatalf("GetUserByToken() error = %v", err)
	}
	if gotUser != user {
		t.Fatalf("GetUserByToken() = %+v, want %+v", gotUser, user)
	}

	gotAuction, err := reloaded.GetAuction(a.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if gotAuction != a {
		t.Fatalf("GetAuction() = %+v, want %+v", gotAuction, a)
	}

	gotBids, err := reloaded.ListBids(a.ID)
	if err != nil {
		t.Fatalf("ListBids() error = %v", err)
	}
	if len(gotBids) != 1 || gotBids[0] != bid {
		t.Fatalf("ListBids() = %+v, want [%+v]", gotBids, bid)
	}

	gotOrder, err := reloaded.GetOrderByAuction(a.ID)
	if err != nil {
		t.Fatalf("GetOrderByAuction() error = %v", err)
	}
	if gotOrder != firstOrder {
		t.Fatalf("GetOrderByAuction() = %+v, want %+v", gotOrder, firstOrder)
	}
}

func TestFileRepositoryGetUserByTokenRejectsExpiredSession(t *testing.T) {
	repo, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	user := auction.User{
		ID:          "user-1",
		Username:    "alice",
		DisplayName: "Alice",
		Role:        auction.RoleBidder,
		Status:      auction.UserActive,
		CreatedAt:   time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:   time.Now().UTC().Add(-2 * time.Hour),
	}
	session := auction.Session{
		Token:     "expired-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}

	if err := repo.SaveUser(user); err != nil {
		t.Fatalf("SaveUser() error = %v", err)
	}
	if err := repo.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	if _, err := repo.GetUserByToken(session.Token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUserByToken(expired) error = %v, want %v", err, ErrNotFound)
	}
}

func TestFileRepositoryPersistsUsersByUsername(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Round(0)

	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}
	user := auction.User{
		ID:           "usr-user-a",
		Username:     "userA",
		DisplayName:  "用户A",
		PasswordHash: "hash-a",
		PasswordSalt: "salt-a",
		Role:         auction.RoleBidder,
		Status:       auction.UserActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.UpsertUser(user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}

	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository(reloaded) error = %v", err)
	}
	got, err := reloaded.GetUserByUsername("userA")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if got != user {
		t.Fatalf("GetUserByUsername() = %+v, want %+v", got, user)
	}
	items, err := reloaded.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(items) != 1 || items[0] != user {
		t.Fatalf("ListUsers() = %+v, want [%+v]", items, user)
	}
}

func TestFileRepositoryUpdateAuctionPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Round(0)

	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	a := auction.Auction{
		ID:         "auction-1",
		MerchantID: "merchant-1",
		Product: auction.Product{
			Name:        "Camera",
			ImageURL:    "https://example.test/camera.jpg",
			Description: "Mirrorless camera",
		},
		Rules: auction.Rules{
			StartPrice:      100,
			Increment:       10,
			Duration:        time.Hour,
			CeilingPrice:    500,
			ExtendThreshold: 5 * time.Minute,
			ExtendBy:        time.Minute,
		},
		Status:       auction.StatusRunning,
		CurrentPrice: 100,
		StartsAt:     now,
		EndsAt:       now.Add(time.Hour),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if _, err := repo.CreateAuction(a); err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}

	updated := a
	updated.Status = auction.StatusEnded
	updated.CurrentPrice = 180
	updated.UpdatedAt = now.Add(30 * time.Minute)
	if err := repo.UpdateAuction(updated); err != nil {
		t.Fatalf("UpdateAuction() error = %v", err)
	}

	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository(reloaded) error = %v", err)
	}
	got, err := reloaded.GetAuction(a.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if got != updated {
		t.Fatalf("GetAuction() = %+v, want %+v", got, updated)
	}
}

func TestFileRepositorySaveBidPersistsMultipleBidsInOrder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Round(0)

	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	bids := []auction.Bid{
		{ID: "bid-1", AuctionID: "auction-1", UserID: "user-1", RequestID: "request-1", Amount: 110, CreatedAt: now},
		{ID: "bid-2", AuctionID: "auction-1", UserID: "user-2", RequestID: "request-2", Amount: 120, CreatedAt: now.Add(time.Minute)},
		{ID: "bid-3", AuctionID: "auction-1", UserID: "user-3", RequestID: "request-3", Amount: 130, CreatedAt: now.Add(2 * time.Minute)},
	}
	for _, bid := range bids {
		if err := repo.SaveBid(bid); err != nil {
			t.Fatalf("SaveBid(%s) error = %v", bid.ID, err)
		}
	}

	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository(reloaded) error = %v", err)
	}
	got, err := reloaded.ListBids("auction-1")
	if err != nil {
		t.Fatalf("ListBids() error = %v", err)
	}
	if len(got) != len(bids) {
		t.Fatalf("ListBids() len = %d, want %d: %+v", len(got), len(bids), got)
	}
	for i := range bids {
		if got[i] != bids[i] {
			t.Fatalf("ListBids()[%d] = %+v, want %+v", i, got[i], bids[i])
		}
	}
}

func TestFileRepositoryFailedAuctionWriteDoesNotCommitToMemory(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, auctionsFile), 0o755); err != nil {
		t.Fatalf("Mkdir(%s) error = %v", auctionsFile, err)
	}

	a := auction.Auction{ID: "auction-1", MerchantID: "merchant-1"}
	if _, err := repo.CreateAuction(a); err == nil {
		t.Fatal("CreateAuction() error = nil, want write failure")
	}
	if _, err := repo.GetAuction(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAuction() error = %v, want %v", err, ErrNotFound)
	}
}
