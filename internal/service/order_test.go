// service 测试覆盖订单结算的幂等生成行为。
package service

import (
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

func TestSettlementCreatesOneOrderForSoldAuction(t *testing.T) {
	repo := repository.NewMemoryRepository()
	settlement := NewSettlementService(repo)
	sold := auction.Auction{
		ID:            "auction-1",
		Product:       auction.Product{Name: "jade"},
		Status:        auction.StatusSold,
		CurrentPrice:  800,
		HighestBidder: "user-1",
		SoldAt:        time.Unix(200, 0),
	}

	first, err := settlement.Settle(sold)
	if err != nil {
		t.Fatalf("first settle: %v", err)
	}
	second, err := settlement.Settle(sold)
	if err != nil {
		t.Fatalf("second settle: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected idempotent order, got %s and %s", first.ID, second.ID)
	}
}

func TestSettlementDoesNotCreateOrderWithoutBid(t *testing.T) {
	repo := repository.NewMemoryRepository()
	settlement := NewSettlementService(repo)
	ended := auction.Auction{ID: "auction-1", Status: auction.StatusEnded}

	if _, err := settlement.Settle(ended); err == nil {
		t.Fatal("expected settlement without buyer to fail")
	}
}
