package repository

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

const (
	auctionsFile = "auctions.json"
	bidsFile     = "bids.json"
	ordersFile   = "orders.json"
	usersFile    = "users.json"
	sessionsFile = "sessions.json"
)

type FileRepository struct {
	mu       sync.Mutex
	dir      string
	users    map[string]auction.User
	sessions map[string]auction.Session
	auctions map[string]auction.Auction
	bids     map[string][]auction.Bid
	orders   map[string]auction.Order
}

func NewFileRepository(dir string) (*FileRepository, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &FileRepository{
		dir:      dir,
		users:    map[string]auction.User{},
		sessions: map[string]auction.Session{},
		auctions: map[string]auction.Auction{},
		bids:     map[string][]auction.Bid{},
		orders:   map[string]auction.Order{},
	}
	if err := r.load(usersFile, &r.users); err != nil {
		return nil, err
	}
	if err := r.load(sessionsFile, &r.sessions); err != nil {
		return nil, err
	}
	if err := r.load(auctionsFile, &r.auctions); err != nil {
		return nil, err
	}
	if err := r.load(bidsFile, &r.bids); err != nil {
		return nil, err
	}
	if err := r.load(ordersFile, &r.orders); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *FileRepository) SaveUser(user auction.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[user.ID] = user
	return r.save(usersFile, r.users)
}

func (r *FileRepository) SaveSession(session auction.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.Token] = session
	return r.save(sessionsFile, r.sessions)
}

func (r *FileRepository) GetUserByToken(token string) (auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[token]
	if !ok || !time.Now().UTC().Before(session.ExpiresAt) {
		return auction.User{}, ErrNotFound
	}
	user, ok := r.users[session.UserID]
	if !ok {
		return auction.User{}, ErrNotFound
	}
	return user, nil
}

func (r *FileRepository) CreateAuction(a auction.Auction) (auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auctions[a.ID] = a
	if err := r.save(auctionsFile, r.auctions); err != nil {
		return auction.Auction{}, err
	}
	return a, nil
}

func (r *FileRepository) UpdateAuction(a auction.Auction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.auctions[a.ID]; !ok {
		return ErrNotFound
	}
	r.auctions[a.ID] = a
	return r.save(auctionsFile, r.auctions)
}

func (r *FileRepository) GetAuction(id string) (auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.auctions[id]
	if !ok {
		return auction.Auction{}, ErrNotFound
	}
	return a, nil
}

func (r *FileRepository) ListAuctions() ([]auction.Auction, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]auction.Auction, 0, len(r.auctions))
	for _, item := range r.auctions {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r *FileRepository) SaveBid(bid auction.Bid) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bids[bid.AuctionID] = append(r.bids[bid.AuctionID], bid)
	return r.save(bidsFile, r.bids)
}

func (r *FileRepository) ListBids(auctionID string) ([]auction.Bid, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bids := r.bids[auctionID]
	out := make([]auction.Bid, len(bids))
	copy(out, bids)
	return out, nil
}

func (r *FileRepository) UpsertOrder(order auction.Order) (auction.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.orders[order.AuctionID]; ok {
		return existing, nil
	}
	r.orders[order.AuctionID] = order
	if err := r.save(ordersFile, r.orders); err != nil {
		return auction.Order{}, err
	}
	return order, nil
}

func (r *FileRepository) GetOrderByAuction(auctionID string) (auction.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[auctionID]
	if !ok {
		return auction.Order{}, ErrNotFound
	}
	return order, nil
}

func (r *FileRepository) load(name string, target any) error {
	data, err := os.ReadFile(filepath.Join(r.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

func (r *FileRepository) save(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(r.dir, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
