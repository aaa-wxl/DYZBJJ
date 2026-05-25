// auction 定义竞拍领域模型、状态机和规则校验。
package auction

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	// StatusDraft 表示竞拍已创建但尚未开始。
	StatusDraft     Status = "DRAFT"
	// StatusScheduled 表示竞拍已排期，后续可按计划启动。
	StatusScheduled Status = "SCHEDULED"
	// StatusRunning 表示竞拍正在接受出价。
	StatusRunning   Status = "RUNNING"
	// StatusExtended 表示竞拍因临近结束出价被自动延时。
	StatusExtended  Status = "EXTENDED"
	// StatusSold 表示竞拍已成交。
	StatusSold      Status = "SOLD"
	// StatusEnded 表示竞拍自然结束但没有成交。
	StatusEnded     Status = "ENDED"
	// StatusCancelled 表示竞拍被商家异常取消。
	StatusCancelled Status = "CANCELLED"
)

var (
	ErrInvalidRules      = errors.New("invalid auction rules")
	ErrInvalidTransition = errors.New("invalid auction state transition")
)

type Product struct {
	Name        string `json:"name"`
	ImageURL    string `json:"imageUrl"`
	Description string `json:"description"`
}

// Rules 描述竞拍规则，金额统一使用整数分或演示用整数单位。
type Rules struct {
	StartPrice      int64         `json:"startPrice"`
	Increment       int64         `json:"increment"`
	Duration        time.Duration `json:"duration"`
	CeilingPrice    int64         `json:"ceilingPrice"`
	ExtendThreshold time.Duration `json:"extendThreshold"`
	ExtendBy        time.Duration `json:"extendBy"`
}

// Validate 校验竞拍规则是否满足创建和启动的最低约束。
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

// Auction 是数据库侧的竞拍聚合根。
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

// Bid 记录一笔已被接受的出价流水。
type Bid struct {
	ID        string    `json:"id"`
	AuctionID string    `json:"auctionId"`
	UserID    string    `json:"userId"`
	RequestID string    `json:"requestId"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

// Order 是竞拍成交后生成的最小订单记录。
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

// ValidateForCreate 校验创建竞拍时必须具备的商家、商品和规则信息。
func (a *Auction) ValidateForCreate() error {
	if strings.TrimSpace(a.MerchantID) == "" {
		return fmt.Errorf("%w: merchant id is required", ErrInvalidRules)
	}
	if strings.TrimSpace(a.Product.Name) == "" {
		return fmt.Errorf("%w: product name is required", ErrInvalidRules)
	}
	return a.Rules.Validate()
}

// Start 将未开始竞拍切换为 RUNNING，并初始化当前价和结束时间。
func (a *Auction) Start(now time.Time) error {
	if err := a.ValidateForCreate(); err != nil {
		return err
	}
	if a.Status != StatusDraft && a.Status != StatusScheduled {
		return fmt.Errorf("%w: cannot start auction in %s", ErrInvalidTransition, a.Status)
	}
	a.Status = StatusRunning
	a.CurrentPrice = a.Rules.StartPrice
	a.StartsAt = now.UTC()
	a.EndsAt = now.UTC().Add(a.Rules.Duration)
	a.UpdatedAt = now.UTC()
	return nil
}

// Cancel 只允许取消尚未结束的竞拍。
func (a *Auction) Cancel() error {
	switch a.Status {
	case StatusDraft, StatusScheduled, StatusRunning, StatusExtended:
		a.Status = StatusCancelled
		a.UpdatedAt = time.Now().UTC()
		return nil
	default:
		return fmt.Errorf("%w: cannot cancel auction in %s", ErrInvalidTransition, a.Status)
	}
}

// Finish 根据是否存在最高出价人决定进入 SOLD 或 ENDED。
func (a *Auction) Finish(now time.Time) error {
	if a.Status != StatusRunning && a.Status != StatusExtended {
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

// ToSnapshot 将数据库竞拍聚合转换为实时房间快照。
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

// IsOpenForBid 判断当前状态是否允许用户继续出价。
func IsOpenForBid(status Status) bool {
	return status == StatusRunning || status == StatusExtended
}

// NextMinimumBid 计算下一次最低有效出价。
func NextMinimumBid(current int64, rules Rules) int64 {
	next := current + rules.Increment
	if next < rules.StartPrice {
		return rules.StartPrice
	}
	return next
}

// NewID 生成演示用唯一 ID；生产环境应替换为稳定 ID 生成器。
func NewID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}
