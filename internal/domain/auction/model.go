package auction

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type Status string

const (
	// StatusDraft 表示竞拍已创建但尚未开始。
	StatusDraft Status = "DRAFT"
	// StatusRunning 表示竞拍正在接受出价。
	StatusRunning Status = "RUNNING"
	// StatusSold 表示竞拍已成交。
	StatusSold Status = "SOLD"
	// StatusEnded 表示竞拍自然结束但没有成交。
	StatusEnded Status = "ENDED"
	// StatusCancelled 表示竞拍已取消。
	StatusCancelled Status = "CANCELLED"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleBidder Role = "bidder"
)

var (
	ErrInvalidRules      = errors.New("invalid auction rules")
	ErrInvalidTransition = errors.New("invalid auction state transition")
	idCounter            uint64
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type Product struct {
	Name        string `json:"name"`
	ImageURL    string `json:"imageUrl"`
	Description string `json:"description"`
}

// Rules 描述竞拍规则，金额使用整数单位。
type Rules struct {
	StartPrice      int64         `json:"startPrice"`
	Increment       int64         `json:"increment"`
	Duration        time.Duration `json:"duration"`
	CeilingPrice    int64         `json:"ceilingPrice"`
	ExtendThreshold time.Duration `json:"extendThreshold"`
	ExtendBy        time.Duration `json:"extendBy"`
}

// Validate 校验竞拍规则是否满足创建和启动约束。
func (r Rules) Validate() error {
	var problems []string
	if r.StartPrice < 0 {
		problems = append(problems, "start price must be non-negative")
	}
	if r.Increment <= 0 {
		problems = append(problems, "increment must be greater than zero")
	}
	if r.Duration <= 0 {
		problems = append(problems, "duration must be greater than zero")
	}
	if r.CeilingPrice < r.StartPrice {
		problems = append(problems, "ceiling price must be greater than or equal to start price")
	}
	if r.ExtendThreshold < 0 {
		problems = append(problems, "extend threshold must be non-negative")
	}
	if r.ExtendBy < 0 {
		problems = append(problems, "extend duration must be non-negative")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalidRules, strings.Join(problems, "; "))
	}
	return nil
}

// Auction 是竞拍聚合根。
type Auction struct {
	ID            string    `json:"id"`
	MerchantID    string    `json:"merchantId"`
	Product       Product   `json:"product"`
	Rules         Rules     `json:"rules"`
	Status        Status    `json:"status"`
	CurrentPrice  int64     `json:"currentPrice"`
	HighestBidder string    `json:"highestBidder,omitempty"`
	StartsAt      time.Time `json:"startsAt,omitempty"`
	EndsAt        time.Time `json:"endsAt,omitempty"`
	SoldAt        time.Time `json:"soldAt,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Snapshot 是实时房间对外暴露的竞拍快照。
type Snapshot struct {
	AuctionID      string    `json:"auctionId"`
	Product        Product   `json:"product"`
	Rules          Rules     `json:"rules"`
	Status         Status    `json:"status"`
	CurrentPrice   int64     `json:"currentPrice"`
	HighestBidder  string    `json:"highestBidder,omitempty"`
	EndsAt         time.Time `json:"endsAt"`
	ServerTime     time.Time `json:"serverTime"`
	Rank           int       `json:"rank,omitempty"`
	Participants   int       `json:"participants"`
	NextMinimumBid int64     `json:"nextMinimumBid"`
}

// Bid 记录一笔已接受的出价流水。
type Bid struct {
	ID        string    `json:"id"`
	AuctionID string    `json:"auctionId"`
	UserID    string    `json:"userId"`
	RequestID string    `json:"requestId"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

// Order 是竞拍成交后生成的订单记录。
type Order struct {
	ID          string    `json:"id"`
	AuctionID   string    `json:"auctionId"`
	ProductName string    `json:"productName"`
	BuyerID     string    `json:"buyerId"`
	FinalPrice  int64     `json:"finalPrice"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

// NewAuction 创建初始 DRAFT 竞拍。
func NewAuction(merchantID string, product Product, rules Rules) Auction {
	now := time.Now().UTC()
	return Auction{
		ID:           NewID("auc"),
		MerchantID:   merchantID,
		Product:      product,
		Rules:        rules,
		Status:       StatusDraft,
		CurrentPrice: rules.StartPrice,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// ValidateForCreate 校验创建竞拍所需的商家、商品和规则信息。
func (a *Auction) ValidateForCreate() error {
	if strings.TrimSpace(a.MerchantID) == "" {
		return fmt.Errorf("%w: merchant id is required", ErrInvalidRules)
	}
	if strings.TrimSpace(a.Product.Name) == "" {
		return fmt.Errorf("%w: product name is required", ErrInvalidRules)
	}
	return a.Rules.Validate()
}

// Start 将 DRAFT 竞拍切换为 RUNNING。
func (a *Auction) Start(now time.Time) error {
	if err := a.ValidateForCreate(); err != nil {
		return err
	}
	if a.Status != StatusDraft {
		return fmt.Errorf("%w: cannot start auction in %s", ErrInvalidTransition, a.Status)
	}
	a.Status = StatusRunning
	a.CurrentPrice = a.Rules.StartPrice
	a.StartsAt = now.UTC()
	a.EndsAt = now.UTC().Add(a.Rules.Duration)
	a.UpdatedAt = now.UTC()
	return nil
}

// Cancel 只允许取消 DRAFT 或 RUNNING 竞拍。
func (a *Auction) Cancel() error {
	switch a.Status {
	case StatusDraft, StatusRunning:
		a.Status = StatusCancelled
		a.UpdatedAt = time.Now().UTC()
		return nil
	default:
		return fmt.Errorf("%w: cannot cancel auction in %s", ErrInvalidTransition, a.Status)
	}
}

// Finish 根据最高出价人决定进入 SOLD 或 ENDED。
func (a *Auction) Finish(now time.Time) error {
	if a.Status != StatusRunning {
		return fmt.Errorf("%w: cannot finish auction in %s", ErrInvalidTransition, a.Status)
	}
	if now.Before(a.EndsAt) {
		return fmt.Errorf("%w: auction has not ended", ErrInvalidTransition)
	}
	if a.HighestBidder == "" {
		a.Status = StatusEnded
	} else {
		a.Status = StatusSold
		a.SoldAt = now.UTC()
	}
	a.UpdatedAt = now.UTC()
	return nil
}

// ToSnapshot 将竞拍聚合转换为实时房间快照。
func (a Auction) ToSnapshot(now time.Time) Snapshot {
	return Snapshot{
		AuctionID:      a.ID,
		Product:        a.Product,
		Rules:          a.Rules,
		Status:         a.Status,
		CurrentPrice:   a.CurrentPrice,
		HighestBidder:  a.HighestBidder,
		EndsAt:         a.EndsAt,
		ServerTime:     now.UTC(),
		NextMinimumBid: NextMinimumBid(a.CurrentPrice, a.Rules),
	}
}

// IsOpenForBid 判断当前状态是否允许继续出价。
func IsOpenForBid(status Status) bool {
	return status == StatusRunning
}

// NextMinimumBid 计算下一次最低有效出价。
func NextMinimumBid(current int64, rules Rules) int64 {
	next := current + rules.Increment
	if next < rules.StartPrice {
		return rules.StartPrice
	}
	return next
}

// NewID 生成演示用唯一 ID。
func NewID(prefix string) string {
	seq := atomic.AddUint64(&idCounter, 1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), seq)
}
