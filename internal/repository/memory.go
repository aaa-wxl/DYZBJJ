// repository 的内存实现用于本地演示和单元测试。
package repository

import (
	"sync"

	"realtime-auction-core/internal/domain/auction"
)

type MemoryRepository struct {
	mu       sync.Mutex
	auctions map[string]auction.Auction
	bids     map[string][]auction.Bid
	orders   map[string]auction.Order
}

// NewMemoryRepository 创建线程安全的内存 repository。
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		auctions: map[string]auction.Auction{},
		bids:     map[string][]auction.Bid{},
		orders:   map[string]auction.Order{},
	}
}

func (r *MemoryRepository) CreateAuction(a auction.Auction) (auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auctions[a.ID] = a
	return a, nil
}

func (r *MemoryRepository) UpdateAuction(a auction.Auction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.auctions[a.ID]; !ok {
		return ErrNotFound
	}
	r.auctions[a.ID] = a
	return nil
}

func (r *MemoryRepository) GetAuction(id string) (auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.auctions[id]
	if !ok {
		return auction.Auction{}, ErrNotFound
	}
	return a, nil
}

func (r *MemoryRepository) ListAuctions() ([]auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]auction.Auction, 0, len(r.auctions))
	for _, item := range r.auctions {
		items = append(items, item)
	}
	return items, nil
}

func (r *MemoryRepository) SaveBid(bid auction.Bid) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bids[bid.AuctionID] = append(r.bids[bid.AuctionID], bid)
	return nil
}

func (r *MemoryRepository) ListBids(auctionID string) ([]auction.Bid, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bids := r.bids[auctionID]
	out := make([]auction.Bid, len(bids))
	copy(out, bids)
	return out, nil
}

func (r *MemoryRepository) UpsertOrder(order auction.Order) (auction.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.orders[order.AuctionID]; ok {
		return existing, nil
	}
	r.orders[order.AuctionID] = order
	return order, nil
}

func (r *MemoryRepository) GetOrderByAuction(auctionID string) (auction.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[auctionID]
	if !ok {
		return auction.Order{}, ErrNotFound
	}
	return order, nil
}
