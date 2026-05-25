// service 提供跨领域的应用服务编排。
package service

import (
	"fmt"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

type SettlementService struct {
	repo repository.AuctionRepository
}

// NewSettlementService 创建订单结算服务。
func NewSettlementService(repo repository.AuctionRepository) *SettlementService {
	return &SettlementService{repo: repo}
}

// Settle 根据 SOLD 竞拍生成最小订单，并依赖 repository 保证幂等。
func (s *SettlementService) Settle(a auction.Auction) (auction.Order, error) {
	if a.Status != auction.StatusSold {
		return auction.Order{}, fmt.Errorf("auction %s is not sold", a.ID)
	}
	if a.HighestBidder == "" {
		return auction.Order{}, fmt.Errorf("auction %s has no buyer", a.ID)
	}
	order := auction.Order{
		ID:          auction.NewID("ord"),
		AuctionID:   a.ID,
		ProductName: a.Product.Name,
		BuyerID:     a.HighestBidder,
		FinalPrice:  a.CurrentPrice,
		Status:      "PENDING_PAYMENT",
		CreatedAt:   time.Now().UTC(),
	}
	return s.repo.UpsertOrder(order)
}
