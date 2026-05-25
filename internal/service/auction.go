// service 编排竞拍 API、实时状态、房间广播和订单结算。
package service

import (
	"fmt"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/ws"
)

type AuctionService struct {
	repo       repository.AuctionRepository
	store      redis.Store
	hub        *ws.Hub
	settlement *SettlementService
}

// NewAuctionService 组装竞拍应用服务依赖。
func NewAuctionService(repo repository.AuctionRepository, store redis.Store, hub *ws.Hub) *AuctionService {
	return &AuctionService{
		repo:       repo,
		store:      store,
		hub:        hub,
		settlement: NewSettlementService(repo),
	}
}

// CreateAuction 创建 DRAFT 竞拍。
func (s *AuctionService) CreateAuction(merchantID string, product auction.Product, rules auction.Rules) (auction.Auction, error) {
	a := auction.NewAuction(merchantID, product, rules)
	if err := a.ValidateForCreate(); err != nil {
		return auction.Auction{}, err
	}
	return s.repo.CreateAuction(a)
}

// ListAuctions 返回当前所有竞拍记录。
func (s *AuctionService) ListAuctions() ([]auction.Auction, error) {
	return s.repo.ListAuctions()
}

// GetAuction 查询单个竞拍聚合。
func (s *AuctionService) GetAuction(id string) (auction.Auction, error) {
	return s.repo.GetAuction(id)
}

// StartAuction 启动竞拍并初始化实时快照。
func (s *AuctionService) StartAuction(id string, now time.Time) (auction.Snapshot, error) {
	a, err := s.repo.GetAuction(id)
	if err != nil {
		return auction.Snapshot{}, err
	}
	if err := a.Start(now); err != nil {
		return auction.Snapshot{}, err
	}
	if err := s.repo.UpdateAuction(a); err != nil {
		return auction.Snapshot{}, err
	}
	snapshot := a.ToSnapshot(now)
	if err := s.store.InitAuction(snapshot); err != nil {
		return auction.Snapshot{}, err
	}
	s.hub.Broadcast(ws.Event{Type: ws.EventSnapshot, AuctionID: id, Snapshot: snapshot})
	return snapshot, nil
}

// CancelAuction 取消未结束竞拍并广播取消事件。
func (s *AuctionService) CancelAuction(id string, now time.Time) (auction.Snapshot, error) {
	a, err := s.repo.GetAuction(id)
	if err != nil {
		return auction.Snapshot{}, err
	}
	if err := a.Cancel(); err != nil {
		return auction.Snapshot{}, err
	}
	if err := s.repo.UpdateAuction(a); err != nil {
		return auction.Snapshot{}, err
	}
	snapshot, err := s.store.Cancel(id, now)
	if err != nil {
		snapshot = a.ToSnapshot(now)
	}
	s.hub.Broadcast(ws.Event{Type: ws.EventAuctionCancelled, AuctionID: id, Snapshot: snapshot, Reason: "merchant_cancelled"})
	return snapshot, nil
}

// Snapshot 返回用户进入或重连房间时的最新状态。
func (s *AuctionService) Snapshot(id, userID string) (auction.Snapshot, error) {
	return s.store.Snapshot(id, userID)
}

// PlaceBid 处理用户出价，并在状态变化后广播事件。
func (s *AuctionService) PlaceBid(command redis.BidCommand) (redis.BidResult, error) {
	result, err := s.store.PlaceBid(command)
	if err != nil {
		return result, err
	}
	if !result.Idempotent {
		_ = s.repo.SaveBid(auction.Bid{
			ID:        result.BidID,
			AuctionID: command.AuctionID,
			UserID:    command.UserID,
			RequestID: command.RequestID,
			Amount:    command.Amount,
			CreatedAt: command.Now.UTC(),
		})
	}
	if err := s.syncAuctionFromSnapshot(result.Snapshot); err != nil {
		return result, err
	}

	eventType := ws.EventBidAccepted
	reason := ""
	if result.Snapshot.Status == auction.StatusSold {
		// 成交事件使用结束广播，前端据此锁定出价入口。
		eventType = ws.EventAuctionEnded
		reason = "ceiling_price_reached"
	} else if result.Extended {
		eventType = ws.EventAuctionExtended
		reason = "bid_near_end"
	}
	s.hub.Broadcast(ws.Event{Type: eventType, AuctionID: command.AuctionID, Snapshot: result.Snapshot, Reason: reason})
	return result, nil
}

// FinishExpired 处理到期竞拍，生成成交或流拍结果。
func (s *AuctionService) FinishExpired(id string, now time.Time) (auction.Snapshot, error) {
	snapshot, err := s.store.FinishExpired(id, now)
	if err != nil {
		return auction.Snapshot{}, err
	}
	if err := s.syncAuctionFromSnapshot(snapshot); err != nil {
		return auction.Snapshot{}, err
	}
	s.hub.Broadcast(ws.Event{Type: ws.EventAuctionEnded, AuctionID: id, Snapshot: snapshot, Reason: "time_expired"})
	return snapshot, nil
}

// GetResult 返回竞拍最终结果和可选订单摘要。
func (s *AuctionService) GetResult(id string) (map[string]any, error) {
	a, err := s.repo.GetAuction(id)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"auction": a, "hasOrder": false}
	order, err := s.repo.GetOrderByAuction(id)
	if err == nil {
		result["order"] = order
		result["hasOrder"] = true
	}
	return result, nil
}

// Subscribe 订阅指定竞拍房间的事件。
func (s *AuctionService) Subscribe(id string) (<-chan ws.Event, func()) {
	return s.hub.Subscribe(id)
}

// syncAuctionFromSnapshot 将实时快照收敛回 repository，并在 SOLD 时触发结算。
func (s *AuctionService) syncAuctionFromSnapshot(snapshot auction.Snapshot) error {
	a, err := s.repo.GetAuction(snapshot.AuctionID)
	if err != nil {
		return err
	}
	a.Status = snapshot.Status
	a.CurrentPrice = snapshot.CurrentPrice
	a.HighestBidder = snapshot.HighestBidder
	a.EndsAt = snapshot.EndsAt
	a.UpdatedAt = snapshot.ServerTime
	if snapshot.Status == auction.StatusSold && !snapshot.ServerTime.IsZero() {
		a.SoldAt = snapshot.ServerTime
	}
	if err := s.repo.UpdateAuction(a); err != nil {
		return err
	}
	if snapshot.Status == auction.StatusSold {
		if _, err := s.settlement.Settle(a); err != nil {
			return fmt.Errorf("settle auction: %w", err)
		}
	}
	return nil
}
