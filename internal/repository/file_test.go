package repository

import (
	"errors"
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
		ID:        "user-1",
		Name:      "Alice",
		Role:      auction.RoleBidder,
		CreatedAt: now,
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
		ID:        "user-1",
		Name:      "Alice",
		Role:      auction.RoleBidder,
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
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
