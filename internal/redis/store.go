// redis 提供竞拍实时路径的原子状态存储接口和内存实现。
package redis

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

var (
	ErrAuctionNotFound = errors.New("auction snapshot not found")
	ErrBidRejected     = errors.New("bid rejected")
)

type BidCommand struct {
	AuctionID string
	UserID    string
	RequestID string
	Amount    int64
	Now       time.Time
}

// BidResult 描述一次出价处理后的快照、下一口价和幂等状态。
type BidResult struct {
	BidID       string
	Snapshot    auction.Snapshot
	NextMinimum int64
	Extended    bool
	Idempotent  bool
}

// Store 抽象 Redis 原子出价能力，便于后续替换为真实 Redis Lua 实现。
type Store interface {
	InitAuction(snapshot auction.Snapshot) error
	PlaceBid(command BidCommand) (BidResult, error)
	Snapshot(auctionID, userID string) (auction.Snapshot, error)
	Cancel(auctionID string, now time.Time) (auction.Snapshot, error)
	FinishExpired(auctionID string, now time.Time) (auction.Snapshot, error)
}

// MemoryStore 使用 mutex 模拟 Redis 原子脚本，适合本地闭环验证和单元测试。
type MemoryStore struct {
	mu        sync.Mutex
	snapshots map[string]auction.Snapshot
	rankings  map[string]map[string]int64
	requests  map[string]BidResult
}

// NewMemoryStore 创建内存实时状态存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		snapshots: map[string]auction.Snapshot{},
		rankings:  map[string]map[string]int64{},
		requests:  map[string]BidResult{},
	}
}

// InitAuction 在竞拍启动时写入实时快照。
func (s *MemoryStore) InitAuction(snapshot auction.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.AuctionID == "" {
		return fmt.Errorf("auction id is required")
	}
	if err := snapshot.Rules.Validate(); err != nil {
		return err
	}
	snapshot.ServerTime = time.Now().UTC()
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	s.snapshots[snapshot.AuctionID] = snapshot
	if _, ok := s.rankings[snapshot.AuctionID]; !ok {
		s.rankings[snapshot.AuctionID] = map[string]int64{}
	}
	return nil
}

// PlaceBid 原子校验并更新一笔出价，保持价格、排行榜和成交状态一致。
func (s *MemoryStore) PlaceBid(command BidCommand) (BidResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	requestKey := AuctionRequestKey(command.AuctionID, command.RequestID)
	if existing, ok := s.requests[requestKey]; ok {
		existing.Idempotent = true
		return existing, nil
	}

	snapshot, ok := s.snapshots[command.AuctionID]
	if !ok {
		return BidResult{}, ErrAuctionNotFound
	}
	if command.Now.IsZero() {
		command.Now = time.Now().UTC()
	}
	if command.UserID == "" || command.RequestID == "" {
		return BidResult{}, fmt.Errorf("%w: user id and request id are required", ErrBidRejected)
	}
	if !auction.IsOpenForBid(snapshot.Status) {
		return BidResult{Snapshot: snapshot, NextMinimum: snapshot.NextMinimumBid}, fmt.Errorf("%w: auction status is %s", ErrBidRejected, snapshot.Status)
	}
	if command.Now.After(snapshot.EndsAt) {
		return BidResult{Snapshot: snapshot, NextMinimum: snapshot.NextMinimumBid}, fmt.Errorf("%w: auction already ended", ErrBidRejected)
	}

	minimum := auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	if command.Amount < minimum {
		return BidResult{Snapshot: snapshot, NextMinimum: minimum}, fmt.Errorf("%w: amount must be at least %d", ErrBidRejected, minimum)
	}
	if (command.Amount-snapshot.Rules.StartPrice)%snapshot.Rules.Increment != 0 {
		return BidResult{Snapshot: snapshot, NextMinimum: minimum}, fmt.Errorf("%w: amount must follow increment", ErrBidRejected)
	}

	extended := false
	snapshot.CurrentPrice = command.Amount
	snapshot.HighestBidder = command.UserID
	snapshot.ServerTime = command.Now.UTC()
	if _, ok := s.rankings[command.AuctionID]; !ok {
		s.rankings[command.AuctionID] = map[string]int64{}
	}
	s.rankings[command.AuctionID][command.UserID] = command.Amount

	if command.Amount >= snapshot.Rules.CeilingPrice {
		// 封顶价优先于自动延时，达到封顶价立即成交。
		snapshot.Status = auction.StatusSold
	} else if snapshot.EndsAt.Sub(command.Now) <= snapshot.Rules.ExtendThreshold && snapshot.Rules.ExtendBy > 0 {
		// 临近结束窗口内的有效出价触发自动延时。
		snapshot.Status = auction.StatusExtended
		snapshot.EndsAt = snapshot.EndsAt.Add(snapshot.Rules.ExtendBy)
		extended = true
	}
	snapshot.Participants = len(s.rankings[command.AuctionID])
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	s.snapshots[command.AuctionID] = snapshot

	result := BidResult{
		BidID:       auction.NewID("bid"),
		Snapshot:    snapshotWithRank(snapshot, s.rankings[command.AuctionID], command.UserID),
		NextMinimum: snapshot.NextMinimumBid,
		Extended:    extended,
	}
	s.requests[requestKey] = result
	return result, nil
}

// Snapshot 返回用户进入或重连房间时需要的最新快照。
func (s *MemoryStore) Snapshot(auctionID, userID string) (auction.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[auctionID]
	if !ok {
		return auction.Snapshot{}, ErrAuctionNotFound
	}
	snapshot.ServerTime = time.Now().UTC()
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	return snapshotWithRank(snapshot, s.rankings[auctionID], userID), nil
}

// Cancel 将实时快照切换为取消状态。
func (s *MemoryStore) Cancel(auctionID string, now time.Time) (auction.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[auctionID]
	if !ok {
		return auction.Snapshot{}, ErrAuctionNotFound
	}
	switch snapshot.Status {
	case auction.StatusDraft, auction.StatusScheduled, auction.StatusRunning, auction.StatusExtended:
		snapshot.Status = auction.StatusCancelled
		snapshot.ServerTime = now.UTC()
		s.snapshots[auctionID] = snapshot
		return snapshot, nil
	default:
		return snapshot, fmt.Errorf("%w: cannot cancel status %s", ErrBidRejected, snapshot.Status)
	}
}

// FinishExpired 在竞拍到期后根据最高出价决定成交或流拍。
func (s *MemoryStore) FinishExpired(auctionID string, now time.Time) (auction.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[auctionID]
	if !ok {
		return auction.Snapshot{}, ErrAuctionNotFound
	}
	if now.Before(snapshot.EndsAt) {
		return snapshot, fmt.Errorf("%w: auction has not ended", ErrBidRejected)
	}
	if snapshot.HighestBidder == "" {
		snapshot.Status = auction.StatusEnded
	} else {
		snapshot.Status = auction.StatusSold
	}
	snapshot.ServerTime = now.UTC()
	s.snapshots[auctionID] = snapshot
	return snapshot, nil
}

// snapshotWithRank 为指定用户补充当前排名。
func snapshotWithRank(snapshot auction.Snapshot, ranking map[string]int64, userID string) auction.Snapshot {
	type pair struct {
		userID string
		amount int64
	}
	var pairs []pair
	for bidder, amount := range ranking {
		pairs = append(pairs, pair{userID: bidder, amount: amount})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].amount > pairs[j].amount
	})
	snapshot.Participants = len(pairs)
	for i, pair := range pairs {
		if pair.userID == userID {
			snapshot.Rank = i + 1
			break
		}
	}
	return snapshot
}
