# Realtime Auction Core Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the existing realtime auction demo into a clean core chain with demo login, PC admin console, mobile H5 bidder flow, file persistence, automatic settlement, and WebSocket recovery.

**Architecture:** Keep the current Go standard-library backend and React/Vite frontend. Tighten the auction state machine in the domain layer, make service logic depend on repository interfaces, add a JSON file repository for local persistence, and split the frontend into `/login`, `/admin`, and `/m` routes.

**Tech Stack:** Go standard library, React 19, TypeScript, Vite, Playwright, local JSON files, WebSocket.

---

## File Structure

- Modify `internal/domain/auction/model.go`: state constants, state transition methods, user/session models.
- Modify `internal/domain/auction/model_test.go`: state machine regression tests.
- Modify `internal/repository/repository.go`: add user/session repository methods.
- Create `internal/repository/file.go`: JSON file backed repository implementation.
- Create `internal/repository/file_test.go`: persistence and reload tests.
- Modify `internal/repository/memory.go`: keep test-friendly repository in sync with interface.
- Modify `internal/redis/store.go`: remove `StatusExtended` writes and keep extension as result/event only.
- Modify `internal/redis/store_test.go`: extension remains `RUNNING`.
- Modify `internal/service/auction.go`: service orchestration, auto-settlement scheduling, role-aware operations.
- Modify `internal/service/settlement.go`: keep order generation idempotent.
- Create `internal/service/auth.go`: demo login/session validation service helpers.
- Modify `internal/service/auction_test.go`: service tests for auto settlement, cancellation, ceiling sale, extension.
- Modify `internal/http/server.go`: routes, auth middleware, structured errors, admin/user API split.
- Modify `internal/http/server_test.go`: login, permissions, admin, bidder, and error tests.
- Modify `cmd/api/main.go`: instantiate file repository by default and seed in-memory realtime store from file data.
- Modify `web/src/api.ts`: typed API client, token handling, WebSocket reconnect helpers.
- Replace `web/src/App.tsx`: route shell for login, admin, and mobile pages.
- Create `web/src/session.ts`: browser session persistence.
- Create `web/src/AdminApp.tsx`: PC admin console.
- Create `web/src/MobileApp.tsx`: mobile H5 bidder flow.
- Create `web/src/LoginPage.tsx`: demo login.
- Replace `web/src/styles.css`: shared layout and responsive styles.
- Modify `web/tests/realtime-ranking.spec.ts`: end-to-end core chain.
- Modify `.gitignore`: ignore `.superpowers/` and local `data/` files created by demos.

## Task 1: Domain State Machine

**Files:**
- Modify: `internal/domain/auction/model.go`
- Modify: `internal/domain/auction/model_test.go`

- [ ] **Step 1: Write failing state tests**

Add these tests to `internal/domain/auction/model_test.go`:

```go
func TestAuctionStateMachineAllowsOnlyCoreTransitions(t *testing.T) {
	a := mustAuction(t)
	if a.Status != StatusDraft {
		t.Fatalf("new auction status = %s, want DRAFT", a.Status)
	}

	if err := a.Cancel(); err != nil {
		t.Fatalf("cancel draft: %v", err)
	}
	if a.Status != StatusCancelled {
		t.Fatalf("status after draft cancel = %s, want CANCELLED", a.Status)
	}

	a = mustAuction(t)
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	if err := a.Start(now); err != nil {
		t.Fatalf("start auction: %v", err)
	}
	if a.Status != StatusRunning {
		t.Fatalf("status after start = %s, want RUNNING", a.Status)
	}
	if err := a.Cancel(); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	if a.Status != StatusCancelled {
		t.Fatalf("status after running cancel = %s, want CANCELLED", a.Status)
	}
}

func TestFinishAuctionChoosesSoldOrEnded(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	ended := mustAuction(t)
	if err := ended.Start(now); err != nil {
		t.Fatal(err)
	}
	if err := ended.Finish(now.Add(ended.Rules.Duration + time.Second)); err != nil {
		t.Fatalf("finish no-bid auction: %v", err)
	}
	if ended.Status != StatusEnded {
		t.Fatalf("no-bid finish status = %s, want ENDED", ended.Status)
	}

	sold := mustAuction(t)
	if err := sold.Start(now); err != nil {
		t.Fatal(err)
	}
	sold.HighestBidder = "user-a"
	sold.CurrentPrice = 100
	if err := sold.Finish(now.Add(sold.Rules.Duration + time.Second)); err != nil {
		t.Fatalf("finish bid auction: %v", err)
	}
	if sold.Status != StatusSold {
		t.Fatalf("bid finish status = %s, want SOLD", sold.Status)
	}
}

func mustAuction(t *testing.T) Auction {
	t.Helper()
	a, err := NewAuction("merchant-demo", Product{Name: "翡翠手镯"}, Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        time.Minute,
		CeilingPrice:    3000,
		ExtendThreshold: 10 * time.Second,
		ExtendBy:        20 * time.Second,
	})
	if err != nil {
		t.Fatalf("new auction: %v", err)
	}
	return a
}
```

- [ ] **Step 2: Run domain tests and verify failure**

Run:

```powershell
go test ./internal/domain/auction
```

Expected: FAIL if `StatusExtended` or `StatusScheduled` behavior is still required by tests or code paths.

- [ ] **Step 3: Update state constants and transitions**

In `internal/domain/auction/model.go`, keep only these state constants:

```go
const (
	StatusDraft     Status = "DRAFT"
	StatusRunning   Status = "RUNNING"
	StatusSold      Status = "SOLD"
	StatusEnded     Status = "ENDED"
	StatusCancelled Status = "CANCELLED"
)
```

Update `Start`, `Cancel`, and `IsOpenForBid`:

```go
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

func IsOpenForBid(status Status) bool {
	return status == StatusRunning
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
go test ./internal/domain/auction
```

Expected: PASS.

Commit:

```powershell
git add internal/domain/auction/model.go internal/domain/auction/model_test.go
git commit -m "refactor: simplify auction state machine"
```

## Task 2: Repository Interface and File Persistence

**Files:**
- Modify: `internal/repository/repository.go`
- Modify: `internal/repository/memory.go`
- Create: `internal/repository/file.go`
- Create: `internal/repository/file_test.go`
- Modify: `internal/domain/auction/model.go`

- [ ] **Step 1: Write failing file repository tests**

Create `internal/repository/file_test.go`:

```go
package repository

import (
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

func TestFileRepositoryPersistsAndReloadsAuctionBidOrderAndSession(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("new file repo: %v", err)
	}

	user := auction.User{ID: "usr-1", Name: "张三", Role: auction.RoleBidder, CreatedAt: time.Now().UTC()}
	session := auction.Session{Token: "token-1", UserID: user.ID, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := repo.SaveUser(user); err != nil {
		t.Fatalf("save user: %v", err)
	}
	if err := repo.SaveSession(session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	a, err := auction.NewAuction("merchant-demo", auction.Product{Name: "翡翠手镯"}, auction.Rules{
		StartPrice: 0, Increment: 100, Duration: time.Minute, CeilingPrice: 3000,
		ExtendThreshold: 10 * time.Second, ExtendBy: 20 * time.Second,
	})
	if err != nil {
		t.Fatalf("new auction: %v", err)
	}
	if _, err := repo.CreateAuction(a); err != nil {
		t.Fatalf("create auction: %v", err)
	}
	if err := repo.SaveBid(auction.Bid{ID: "bid-1", AuctionID: a.ID, UserID: user.ID, RequestID: "req-1", Amount: 100, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("save bid: %v", err)
	}
	order := auction.Order{ID: "ord-1", AuctionID: a.ID, ProductName: a.Product.Name, BuyerID: user.ID, FinalPrice: 100, Status: "PENDING_PAYMENT", CreatedAt: time.Now().UTC()}
	if _, err := repo.UpsertOrder(order); err != nil {
		t.Fatalf("upsert order: %v", err)
	}

	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("reload file repo: %v", err)
	}
	gotUser, err := reloaded.GetUserByToken("token-1")
	if err != nil || gotUser.ID != user.ID {
		t.Fatalf("get user by token = %#v, %v", gotUser, err)
	}
	gotAuction, err := reloaded.GetAuction(a.ID)
	if err != nil || gotAuction.ID != a.ID {
		t.Fatalf("get auction = %#v, %v", gotAuction, err)
	}
	bids, err := reloaded.ListBids(a.ID)
	if err != nil || len(bids) != 1 || bids[0].RequestID != "req-1" {
		t.Fatalf("list bids = %#v, %v", bids, err)
	}
	gotOrder, err := reloaded.GetOrderByAuction(a.ID)
	if err != nil || gotOrder.ID != order.ID {
		t.Fatalf("get order = %#v, %v", gotOrder, err)
	}
}

func TestFileRepositoryOrderUpsertIsIdempotent(t *testing.T) {
	repo, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := auction.Order{ID: "ord-1", AuctionID: "auc-1", BuyerID: "user-a", FinalPrice: 100}
	second := auction.Order{ID: "ord-2", AuctionID: "auc-1", BuyerID: "user-b", FinalPrice: 200}

	if _, err := repo.UpsertOrder(first); err != nil {
		t.Fatal(err)
	}
	got, err := repo.UpsertOrder(second)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.BuyerID != first.BuyerID {
		t.Fatalf("idempotent order = %#v, want first order", got)
	}
}
```

- [ ] **Step 2: Run repository tests and verify failure**

Run:

```powershell
go test ./internal/repository
```

Expected: FAIL with `undefined: NewFileRepository` and missing auth methods.

- [ ] **Step 3: Add user/session domain types**

Append to `internal/domain/auction/model.go`:

```go
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleBidder Role = "bidder"
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
```

- [ ] **Step 4: Extend repository interface**

Update `internal/repository/repository.go`:

```go
type AuctionRepository interface {
	CreateAuction(auction.Auction) (auction.Auction, error)
	UpdateAuction(auction.Auction) error
	GetAuction(id string) (auction.Auction, error)
	ListAuctions() ([]auction.Auction, error)
	SaveBid(auction.Bid) error
	ListBids(auctionID string) ([]auction.Bid, error)
	UpsertOrder(auction.Order) (auction.Order, error)
	GetOrderByAuction(auctionID string) (auction.Order, error)
	SaveUser(auction.User) error
	SaveSession(auction.Session) error
	GetUserByToken(token string) (auction.User, error)
}
```

- [ ] **Step 5: Update memory repository**

Add `users` and `sessions` maps to `MemoryRepository` and implement:

```go
func (r *MemoryRepository) SaveUser(user auction.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[user.ID] = user
	return nil
}

func (r *MemoryRepository) SaveSession(session auction.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.Token] = session
	return nil
}

func (r *MemoryRepository) GetUserByToken(token string) (auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[token]
	if !ok || time.Now().UTC().After(session.ExpiresAt) {
		return auction.User{}, ErrNotFound
	}
	user, ok := r.users[session.UserID]
	if !ok {
		return auction.User{}, ErrNotFound
	}
	return user, nil
}
```

Import `time` in `memory.go`.

- [ ] **Step 6: Implement file repository**

Create `internal/repository/file.go` with this shape:

```go
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

type FileRepository struct {
	mu       sync.Mutex
	dir      string
	auctions map[string]auction.Auction
	bids     map[string][]auction.Bid
	orders   map[string]auction.Order
	users    map[string]auction.User
	sessions map[string]auction.Session
}

func NewFileRepository(dir string) (*FileRepository, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &FileRepository{
		dir: dir, auctions: map[string]auction.Auction{}, bids: map[string][]auction.Bid{},
		orders: map[string]auction.Order{}, users: map[string]auction.User{}, sessions: map[string]auction.Session{},
	}
	if err := r.load("auctions.json", &r.auctions); err != nil { return nil, err }
	if err := r.load("bids.json", &r.bids); err != nil { return nil, err }
	if err := r.load("orders.json", &r.orders); err != nil { return nil, err }
	if err := r.load("users.json", &r.users); err != nil { return nil, err }
	if err := r.load("sessions.json", &r.sessions); err != nil { return nil, err }
	return r, nil
}

func (r *FileRepository) load(name string, target any) error {
	data, err := os.ReadFile(filepath.Join(r.dir, name))
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return err }
	if len(data) == 0 { return nil }
	return json.Unmarshal(data, target)
}

func (r *FileRepository) flush(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil { return err }
	tmp := filepath.Join(r.dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil { return err }
	return os.Rename(tmp, filepath.Join(r.dir, name))
}
```

Then implement the same methods as `MemoryRepository`, calling `flush` after mutations. In `ListAuctions`, sort by `CreatedAt` descending:

```go
sort.Slice(items, func(i, j int) bool {
	return items[i].CreatedAt.After(items[j].CreatedAt)
})
```

- [ ] **Step 7: Run tests and commit**

Run:

```powershell
go test ./internal/repository
go test ./internal/...
```

Expected: PASS after updating all interface callers.

Commit:

```powershell
git add internal/domain/auction/model.go internal/repository/repository.go internal/repository/memory.go internal/repository/file.go internal/repository/file_test.go
git commit -m "feat: add file-backed auction repository"
```

## Task 3: Realtime Store Extension Semantics

**Files:**
- Modify: `internal/redis/store.go`
- Modify: `internal/redis/store_test.go`

- [ ] **Step 1: Add failing extension test**

Add to `internal/redis/store_test.go`:

```go
func TestPlaceBidExtendsWithoutChangingRunningStatus(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	snapshot := auction.Snapshot{
		AuctionID: "auc-1",
		Product: auction.Product{Name: "翡翠手镯"},
		Rules: auction.Rules{
			StartPrice: 0, Increment: 100, Duration: time.Minute, CeilingPrice: 1000,
			ExtendThreshold: 30 * time.Second, ExtendBy: 20 * time.Second,
		},
		Status: auction.StatusRunning,
		CurrentPrice: 0,
		EndsAt: now.Add(10 * time.Second),
	}
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}
	result, err := store.PlaceBid(BidCommand{AuctionID: "auc-1", UserID: "user-a", RequestID: "req-1", Amount: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Extended {
		t.Fatalf("Extended = false, want true")
	}
	if result.Snapshot.Status != auction.StatusRunning {
		t.Fatalf("status = %s, want RUNNING", result.Snapshot.Status)
	}
	if !result.Snapshot.EndsAt.Equal(now.Add(30 * time.Second)) {
		t.Fatalf("endsAt = %s, want %s", result.Snapshot.EndsAt, now.Add(30*time.Second))
	}
}
```

- [ ] **Step 2: Run redis tests and verify failure**

Run:

```powershell
go test ./internal/redis
```

Expected: FAIL if extension currently writes `StatusExtended`.

- [ ] **Step 3: Keep status running on extension**

In `internal/redis/store.go`, replace the extension branch with:

```go
} else if snapshot.EndsAt.Sub(command.Now) <= snapshot.Rules.ExtendThreshold && snapshot.Rules.ExtendBy > 0 {
	snapshot.EndsAt = snapshot.EndsAt.Add(snapshot.Rules.ExtendBy)
	extended = true
}
```

Update `Cancel` to allow only `StatusDraft` and `StatusRunning`:

```go
switch snapshot.Status {
case auction.StatusDraft, auction.StatusRunning:
	snapshot.Status = auction.StatusCancelled
```

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
go test ./internal/redis ./internal/domain/auction
```

Expected: PASS.

Commit:

```powershell
git add internal/redis/store.go internal/redis/store_test.go
git commit -m "refactor: model auction extension as event"
```

## Task 4: Demo Auth Service

**Files:**
- Create: `internal/service/auth.go`
- Create: `internal/service/auth_test.go`

- [ ] **Step 1: Write auth tests**

Create `internal/service/auth_test.go`:

```go
package service

import (
	"testing"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

func TestAuthServiceLoginAndValidateRole(t *testing.T) {
	repo := repository.NewMemoryRepository()
	auth := NewAuthService(repo)

	session, err := auth.Login("李四", auction.RoleBidder)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	user, err := auth.Require(session.Token, auction.RoleBidder)
	if err != nil {
		t.Fatalf("require bidder: %v", err)
	}
	if user.Name != "李四" || user.Role != auction.RoleBidder {
		t.Fatalf("user = %#v", user)
	}
	if _, err := auth.Require(session.Token, auction.RoleAdmin); err == nil {
		t.Fatalf("require admin with bidder token succeeded")
	}
}

func TestAuthServiceRejectsInvalidRole(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository())
	if _, err := auth.Login("王五", auction.Role("owner")); err == nil {
		t.Fatalf("invalid role login succeeded")
	}
}
```

- [ ] **Step 2: Run auth tests and verify failure**

Run:

```powershell
go test ./internal/service -run Auth
```

Expected: FAIL with `undefined: NewAuthService`.

- [ ] **Step 3: Implement auth service**

Create `internal/service/auth.go`:

```go
package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/repository"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type AuthService struct {
	repo repository.AuctionRepository
}

type LoginSession struct {
	Token string       `json:"token"`
	User  auction.User `json:"user"`
}

func NewAuthService(repo repository.AuctionRepository) *AuthService {
	return &AuthService{repo: repo}
}

func (s *AuthService) Login(name string, role auction.Role) (LoginSession, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return LoginSession{}, fmt.Errorf("name is required")
	}
	if role != auction.RoleAdmin && role != auction.RoleBidder {
		return LoginSession{}, fmt.Errorf("invalid role: %s", role)
	}
	now := time.Now().UTC()
	user := auction.User{ID: auction.NewID("usr"), Name: name, Role: role, CreatedAt: now}
	token, err := randomToken()
	if err != nil {
		return LoginSession{}, err
	}
	session := auction.Session{Token: token, UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	if err := s.repo.SaveUser(user); err != nil {
		return LoginSession{}, err
	}
	if err := s.repo.SaveSession(session); err != nil {
		return LoginSession{}, err
	}
	return LoginSession{Token: token, User: user}, nil
}

func (s *AuthService) Require(token string, role auction.Role) (auction.User, error) {
	if strings.TrimSpace(token) == "" {
		return auction.User{}, ErrUnauthorized
	}
	user, err := s.repo.GetUserByToken(token)
	if err != nil {
		return auction.User{}, ErrUnauthorized
	}
	if user.Role != role {
		return auction.User{}, ErrForbidden
	}
	return user, nil
}

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
go test ./internal/service -run Auth
```

Expected: PASS.

Commit:

```powershell
git add internal/service/auth.go internal/service/auth_test.go
git commit -m "feat: add demo authentication service"
```

## Task 5: Service Auto Settlement

**Files:**
- Modify: `internal/service/auction.go`
- Modify: `internal/service/auction_test.go`

- [ ] **Step 1: Write service behavior tests**

Add to `internal/service/auction_test.go`:

```go
func TestStartAuctionSchedulesAutomaticSettlement(t *testing.T) {
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	svc := NewAuctionService(repo, store, hub)

	a, err := svc.CreateAuction("merchant-demo", auction.Product{Name: "翡翠手镯"}, auction.Rules{
		StartPrice: 0, Increment: 100, Duration: 20 * time.Millisecond, CeilingPrice: 3000,
		ExtendThreshold: 0, ExtendBy: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAuction(a.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	got, err := repo.GetAuction(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != auction.StatusEnded {
		t.Fatalf("status = %s, want ENDED", got.Status)
	}
}

func TestManagementFinishIsNotRequiredForSoldAuction(t *testing.T) {
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	svc := NewAuctionService(repo, store, hub)
	now := time.Now().UTC()
	a, err := svc.CreateAuction("merchant-demo", auction.Product{Name: "翡翠手镯"}, auction.Rules{
		StartPrice: 0, Increment: 100, Duration: time.Minute, CeilingPrice: 100,
		ExtendThreshold: 10 * time.Second, ExtendBy: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartAuction(a.ID, now); err != nil {
		t.Fatal(err)
	}
	result, err := svc.PlaceBid(redis.BidCommand{AuctionID: a.ID, UserID: "user-a", RequestID: "req-1", Amount: 100, Now: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Status != auction.StatusSold {
		t.Fatalf("status = %s, want SOLD", result.Snapshot.Status)
	}
	order, err := repo.GetOrderByAuction(a.ID)
	if err != nil {
		t.Fatalf("order missing: %v", err)
	}
	if order.BuyerID != "user-a" || order.FinalPrice != 100 {
		t.Fatalf("order = %#v", order)
	}
}
```

- [ ] **Step 2: Run service tests and verify failure**

Run:

```powershell
go test ./internal/service
```

Expected: FAIL if no automatic settlement scheduling exists.

- [ ] **Step 3: Add automatic settlement scheduling**

In `internal/service/auction.go`, add fields:

```go
timersMu sync.Mutex
timers   map[string]*time.Timer
```

Initialize in `NewAuctionService`:

```go
timers: map[string]*time.Timer{},
```

After successful `StartAuction`, call:

```go
s.scheduleFinish(id, snapshot.EndsAt)
```

Add method:

```go
func (s *AuctionService) scheduleFinish(id string, endsAt time.Time) {
	delay := time.Until(endsAt)
	if delay < 0 {
		delay = 0
	}
	s.timersMu.Lock()
	if existing := s.timers[id]; existing != nil {
		existing.Stop()
	}
	s.timers[id] = time.AfterFunc(delay, func() {
		_, _ = s.FinishExpired(id, time.Now().UTC())
	})
	s.timersMu.Unlock()
}
```

When `PlaceBid` returns `result.Extended`, call:

```go
if result.Extended {
	s.scheduleFinish(command.AuctionID, result.Snapshot.EndsAt)
}
```

When `SOLD`, `ENDED`, or `CANCELLED`, stop and remove the timer:

```go
func (s *AuctionService) stopFinishTimer(id string) {
	s.timersMu.Lock()
	defer s.timersMu.Unlock()
	if timer := s.timers[id]; timer != nil {
		timer.Stop()
		delete(s.timers, id)
	}
}
```

- [ ] **Step 4: Run tests and commit**

Run:

```powershell
go test ./internal/service ./internal/redis ./internal/domain/auction
```

Expected: PASS.

Commit:

```powershell
git add internal/service/auction.go internal/service/auction_test.go
git commit -m "feat: settle auctions automatically"
```

## Task 6: HTTP API, Auth, and Structured Errors

**Files:**
- Modify: `internal/http/server.go`
- Modify: `internal/http/server_test.go`
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Write HTTP tests**

Add tests to `internal/http/server_test.go`:

```go
func TestLoginAndRoleProtectedAdminCreate(t *testing.T) {
	ts := newTestServer(t)

	bidder := postJSON(t, ts, "/api/login", map[string]any{"name": "用户A", "role": "bidder"}, "")
	if bidder.Code != http.StatusOK {
		t.Fatalf("bidder login status = %d body=%s", bidder.Code, bidder.Body.String())
	}
	var bidderLogin service.LoginSession
	decodeBody(t, bidder.Body.Bytes(), &bidderLogin)

	blocked := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), bidderLogin.Token)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("bidder admin create status = %d body=%s", blocked.Code, blocked.Body.String())
	}

	admin := postJSON(t, ts, "/api/login", map[string]any{"name": "管理员", "role": "admin"}, "")
	var adminLogin service.LoginSession
	decodeBody(t, admin.Body.Bytes(), &adminLogin)
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), adminLogin.Token)
	if created.Code != http.StatusCreated {
		t.Fatalf("admin create status = %d body=%s", created.Code, created.Body.String())
	}
}

func TestBidTooLowReturnsStructuredError(t *testing.T) {
	ts := newTestServer(t)
	admin := loginAs(t, ts, "管理员", auction.RoleAdmin)
	bidder := loginAs(t, ts, "用户A", auction.RoleBidder)
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), admin.Token)
	var a auction.Auction
	decodeBody(t, created.Body.Bytes(), &a)
	started := postJSON(t, ts, "/api/admin/auctions/"+a.ID+"/start", nil, admin.Token)
	if started.Code != http.StatusOK {
		t.Fatalf("start status = %d body=%s", started.Code, started.Body.String())
	}

	low := postJSON(t, ts, "/api/auctions/"+a.ID+"/bids", map[string]any{"requestId": "req-1", "amount": 1}, bidder.Token)
	if low.Code != http.StatusBadRequest {
		t.Fatalf("low bid status = %d body=%s", low.Code, low.Body.String())
	}
	var errBody map[string]any
	decodeBody(t, low.Body.Bytes(), &errBody)
	if errBody["code"] != "BID_STEP_INVALID" && errBody["code"] != "BID_TOO_LOW" {
		t.Fatalf("error code = %#v", errBody["code"])
	}
}
```

Helper functions in the same file:

```go
type testResponse struct {
	Code int
	Body *bytes.Buffer
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	auctionSvc := service.NewAuctionService(repo, store, hub)
	authSvc := service.NewAuthService(repo)
	return httptest.NewServer(NewServer(auctionSvc, authSvc).Handler())
}

func postJSON(t *testing.T, ts *httptest.Server, path string, payload any, token string) testResponse {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil { t.Fatal(err) }
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, body)
	if err != nil { t.Fatal(err) }
	req.Header.Set("Content-Type", "application/json")
	if token != "" { req.Header.Set("Authorization", "Bearer "+token) }
	res, err := ts.Client().Do(req)
	if err != nil { t.Fatal(err) }
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, res.Body); err != nil { t.Fatal(err) }
	return testResponse{Code: res.StatusCode, Body: buf}
}
```

- [ ] **Step 2: Run HTTP tests and verify failure**

Run:

```powershell
go test ./internal/http
```

Expected: FAIL because `NewServer` does not accept auth service and routes do not exist.

- [ ] **Step 3: Update server constructor and routes**

Change `Server` in `internal/http/server.go`:

```go
type Server struct {
	service *service.AuctionService
	auth    *service.AuthService
	mux     *nethttp.ServeMux
}

func NewServer(auctionService *service.AuctionService, authService *service.AuthService) *Server {
	s := &Server{service: auctionService, auth: authService, mux: nethttp.NewServeMux()}
	s.routes()
	return s
}
```

Register routes:

```go
s.mux.HandleFunc("POST /api/login", s.login)
s.mux.HandleFunc("GET /api/admin/auctions", s.adminListAuctions)
s.mux.HandleFunc("POST /api/admin/auctions", s.adminCreateAuction)
s.mux.HandleFunc("POST /api/admin/auctions/{id}/start", s.adminStartAuction)
s.mux.HandleFunc("POST /api/admin/auctions/{id}/cancel", s.adminCancelAuction)
s.mux.HandleFunc("GET /api/auctions", s.listAuctions)
s.mux.HandleFunc("GET /api/auctions/{id}/snapshot", s.snapshot)
s.mux.HandleFunc("POST /api/auctions/{id}/bids", s.placeBid)
s.mux.HandleFunc("GET /api/auctions/{id}/result", s.result)
s.mux.HandleFunc("GET /ws/auctions/{id}", s.websocketEvents)
```

Implement auth helpers:

```go
func bearerToken(r *nethttp.Request) string {
	value := r.Header.Get("Authorization")
	if strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	return ""
}

func (s *Server) require(w nethttp.ResponseWriter, r *nethttp.Request, role auction.Role) (auction.User, bool) {
	user, err := s.auth.Require(bearerToken(r), role)
	if err == nil {
		return user, true
	}
	if errors.Is(err, service.ErrForbidden) {
		writeAPIError(w, nethttp.StatusForbidden, "FORBIDDEN", "当前用户无权执行该操作", nil)
		return auction.User{}, false
	}
	writeAPIError(w, nethttp.StatusUnauthorized, "UNAUTHORIZED", "请先登录", nil)
	return auction.User{}, false
}
```

Implement structured error writer:

```go
func writeAPIError(w nethttp.ResponseWriter, status int, code, message string, details any) {
	payload := map[string]any{"code": code, "message": message}
	if details != nil {
		payload["details"] = details
	}
	writeJSON(w, status, payload)
}
```

- [ ] **Step 4: Update bid handler to derive user from token**

In `placeBid`, require bidder and ignore client-supplied `userId`:

```go
user, ok := s.require(w, r, auction.RoleBidder)
if !ok {
	return
}
var req bidRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
	writeAPIError(w, nethttp.StatusBadRequest, "BAD_REQUEST", "请求体格式错误", nil)
	return
}
result, err := s.service.PlaceBid(redis.BidCommand{
	AuctionID: r.PathValue("id"),
	UserID: user.ID,
	RequestID: req.RequestID,
	Amount: req.Amount,
	Now: time.Now().UTC(),
})
```

- [ ] **Step 5: Wire file repository in main**

In `cmd/api/main.go`:

```go
repo, err := repository.NewFileRepository("data")
if err != nil {
	log.Fatal(err)
}
store := redis.NewMemoryStore()
hub := ws.NewHub()
auctionService := service.NewAuctionService(repo, store, hub)
authService := service.NewAuthService(repo)
server := apphttp.NewServer(auctionService, authService)
```

- [ ] **Step 6: Run tests and commit**

Run:

```powershell
go test ./internal/http ./internal/service ./internal/repository ./internal/redis ./internal/domain/auction
```

Expected: PASS.

Commit:

```powershell
git add internal/http/server.go internal/http/server_test.go cmd/api/main.go
git commit -m "feat: add authenticated auction api"
```

## Task 7: Frontend API and Session

**Files:**
- Modify: `web/src/api.ts`
- Create: `web/src/session.ts`

- [ ] **Step 1: Build typed session helper**

Create `web/src/session.ts`:

```ts
import type { User } from "./api";

const KEY = "auction-demo-session";

export type Session = {
  token: string;
  user: User;
};

export function loadSession(): Session | null {
  const raw = window.localStorage.getItem(KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as Session;
  } catch {
    window.localStorage.removeItem(KEY);
    return null;
  }
}

export function saveSession(session: Session) {
  window.localStorage.setItem(KEY, JSON.stringify(session));
}

export function clearSession() {
  window.localStorage.removeItem(KEY);
}
```

- [ ] **Step 2: Replace API client types and request helper**

In `web/src/api.ts`, include these types:

```ts
export type Role = "admin" | "bidder";

export type User = {
  id: string;
  name: string;
  role: Role;
  createdAt: string;
};

export type LoginResponse = {
  token: string;
  user: User;
};

export type APIErrorBody = {
  code: string;
  message: string;
  details?: Record<string, unknown>;
};
```

Use an authenticated request helper:

```ts
async function request<T>(path: string, init: RequestInit = {}, token?: string): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(`${API_BASE}${path}`, { ...init, headers: { ...headers, ...(init.headers ?? {}) } });
  const data = await res.json();
  if (!res.ok) {
    const err = data as APIErrorBody;
    throw new Error(err.message || err.code || "请求失败");
  }
  return data;
}
```

Add functions:

```ts
export function login(name: string, role: Role): Promise<LoginResponse> {
  return request("/api/login", { method: "POST", body: JSON.stringify({ name, role }) });
}

export function adminListAuctions(token: string): Promise<Auction[]> {
  return request("/api/admin/auctions", {}, token);
}

export function adminCreateAuction(token: string, payload: CreateAuctionPayload): Promise<Auction> {
  return request("/api/admin/auctions", { method: "POST", body: JSON.stringify(payload) }, token);
}
```

- [ ] **Step 3: Build frontend**

Run:

```powershell
cd web
npm run build
```

Expected: FAIL until pages are updated to use new API names.

Commit after Task 8, because this task intentionally breaks the old monolithic `App.tsx`.

## Task 8: Frontend Route Shell and Login Page

**Files:**
- Modify: `web/src/App.tsx`
- Create: `web/src/LoginPage.tsx`
- Modify: `web/src/main.tsx`

- [ ] **Step 1: Implement route shell**

Replace `web/src/App.tsx`:

```tsx
import { useMemo, useState } from "react";
import { AdminApp } from "./AdminApp";
import { LoginPage } from "./LoginPage";
import { MobileApp } from "./MobileApp";
import { clearSession, loadSession, saveSession, Session } from "./session";

export function App() {
  const [session, setSession] = useState<Session | null>(() => loadSession());
  const path = useMemo(() => window.location.pathname, []);

  if (!session) {
    return <LoginPage onLogin={(next) => { saveSession(next); setSession(next); }} />;
  }

  const logout = () => {
    clearSession();
    setSession(null);
  };

  if (path.startsWith("/admin")) {
    return <AdminApp session={session} onLogout={logout} />;
  }

  return <MobileApp session={session} onLogout={logout} />;
}
```

- [ ] **Step 2: Implement login page**

Create `web/src/LoginPage.tsx`:

```tsx
import { FormEvent, useState } from "react";
import { login, Role } from "./api";
import type { Session } from "./session";

export function LoginPage({ onLogin }: { onLogin: (session: Session) => void }) {
  const [name, setName] = useState("用户A");
  const [role, setRole] = useState<Role>("bidder");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      onLogin(await login(name, role));
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="login-shell">
      <form className="login-panel" onSubmit={submit}>
        <p className="eyebrow">实时竞拍大师</p>
        <h1>演示登录</h1>
        <label>昵称<input value={name} onChange={(e) => setName(e.target.value)} /></label>
        <div className="segmented">
          <button type="button" className={role === "bidder" ? "active" : ""} onClick={() => setRole("bidder")}>用户端</button>
          <button type="button" className={role === "admin" ? "active" : ""} onClick={() => setRole("admin")}>管理端</button>
        </div>
        {error && <p className="error">{error}</p>}
        <button className="primary" disabled={loading}>{loading ? "登录中..." : "进入系统"}</button>
      </form>
    </main>
  );
}
```

- [ ] **Step 3: Build and keep expected failures scoped**

Run:

```powershell
cd web
npm run build
```

Expected: FAIL with missing `AdminApp` and `MobileApp`; these are implemented in the next two tasks.

Do not commit yet.

## Task 9: Admin Console

**Files:**
- Create: `web/src/AdminApp.tsx`
- Modify: `web/src/api.ts`

- [ ] **Step 1: Implement admin API functions**

Ensure `web/src/api.ts` exports:

```ts
export type CreateAuctionPayload = {
  merchantId: string;
  productName: string;
  imageUrl: string;
  description: string;
  startPrice: number;
  increment: number;
  durationSeconds: number;
  ceilingPrice: number;
  extendThresholdSeconds: number;
  extendBySeconds: number;
};

export function adminStartAuction(token: string, id: string): Promise<Snapshot> {
  return request(`/api/admin/auctions/${id}/start`, { method: "POST" }, token);
}

export function adminCancelAuction(token: string, id: string): Promise<Snapshot> {
  return request(`/api/admin/auctions/${id}/cancel`, { method: "POST" }, token);
}
```

- [ ] **Step 2: Implement admin console**

Create `web/src/AdminApp.tsx`:

```tsx
import { useEffect, useState } from "react";
import { adminCancelAuction, adminCreateAuction, adminListAuctions, adminStartAuction, Auction, CreateAuctionPayload } from "./api";
import type { Session } from "./session";

const initialForm: CreateAuctionPayload = {
  merchantId: "merchant-demo",
  productName: "星河翡翠手镯",
  imageUrl: "https://images.unsplash.com/photo-1617038260897-41a1f14a8ca0?auto=format&fit=crop&w=900&q=80",
  description: "直播间限时竞拍样品，用于演示封顶成交和自动延时。",
  startPrice: 0,
  increment: 100,
  durationSeconds: 180,
  ceilingPrice: 3000,
  extendThresholdSeconds: 20,
  extendBySeconds: 30
};

export function AdminApp({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const [items, setItems] = useState<Auction[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [form, setForm] = useState(initialForm);
  const [message, setMessage] = useState("");

  useEffect(() => { void refresh(); }, []);

  async function refresh() {
    const next = await adminListAuctions(session.token);
    setItems(next);
    if (!selectedId && next[0]) setSelectedId(next[0].id);
  }

  async function run(label: string, action: () => Promise<unknown>) {
    setMessage(`${label}中...`);
    try {
      await action();
      await refresh();
      setMessage(`${label}成功`);
    } catch (err) {
      setMessage(err instanceof Error ? err.message : `${label}失败`);
    }
  }

  const selected = items.find((item) => item.id === selectedId);

  return (
    <main className="admin-shell">
      <aside className="admin-list">
        <div className="bar"><strong>管理端</strong><button onClick={onLogout}>退出</button></div>
        {items.map((item) => (
          <button key={item.id} className={item.id === selectedId ? "row active" : "row"} onClick={() => setSelectedId(item.id)}>
            <span>{item.product.name}</span><em>{item.status}</em>
          </button>
        ))}
      </aside>
      <section className="admin-workspace">
        <div className="bar"><h1>竞拍控制台</h1><p>{session.user.name}</p></div>
        {message && <div className="notice">{message}</div>}
        <div className="admin-grid">
          <form className="panel" onSubmit={(e) => { e.preventDefault(); void run("创建竞拍", () => adminCreateAuction(session.token, form)); }}>
            <h2>发布竞拍</h2>
            <label>商品名称<input value={form.productName} onChange={(e) => setForm({ ...form, productName: e.target.value })} /></label>
            <label>商品图片<input value={form.imageUrl} onChange={(e) => setForm({ ...form, imageUrl: e.target.value })} /></label>
            <label>商品介绍<textarea value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} /></label>
            <div className="number-grid">
              <NumberField label="起拍价" value={form.startPrice} onChange={(v) => setForm({ ...form, startPrice: v })} />
              <NumberField label="加价幅度" value={form.increment} onChange={(v) => setForm({ ...form, increment: v })} />
              <NumberField label="时长(秒)" value={form.durationSeconds} onChange={(v) => setForm({ ...form, durationSeconds: v })} />
              <NumberField label="封顶价" value={form.ceilingPrice} onChange={(v) => setForm({ ...form, ceilingPrice: v })} />
              <NumberField label="延时窗口(秒)" value={form.extendThresholdSeconds} onChange={(v) => setForm({ ...form, extendThresholdSeconds: v })} />
              <NumberField label="延长时长(秒)" value={form.extendBySeconds} onChange={(v) => setForm({ ...form, extendBySeconds: v })} />
            </div>
            <button className="primary">创建竞拍</button>
          </form>
          <div className="panel">
            <h2>当前竞拍</h2>
            {selected ? (
              <>
                <h3>{selected.product.name}</h3>
                <p>{selected.product.description}</p>
                <div className="metrics"><b>状态 {selected.status}</b><b>当前价 ¥{selected.currentPrice}</b></div>
                <div className="actions">
                  <button disabled={selected.status !== "DRAFT"} onClick={() => void run("启动竞拍", () => adminStartAuction(session.token, selected.id))}>启动</button>
                  <button disabled={!["DRAFT", "RUNNING"].includes(selected.status)} onClick={() => void run("取消竞拍", () => adminCancelAuction(session.token, selected.id))}>取消</button>
                </div>
              </>
            ) : <p>暂无竞拍</p>}
          </div>
        </div>
      </section>
    </main>
  );
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label>{label}<input type="number" value={value} onChange={(e) => onChange(Number(e.target.value))} /></label>;
}
```

- [ ] **Step 3: Build and keep expected failure scoped**

Run:

```powershell
cd web
npm run build
```

Expected: FAIL with missing `MobileApp`.

Do not commit yet.

## Task 10: Mobile H5 Bidder Flow

**Files:**
- Create: `web/src/MobileApp.tsx`
- Modify: `web/src/api.ts`
- Modify: `web/src/styles.css`

- [ ] **Step 1: Add bidder API functions and WebSocket helper**

In `web/src/api.ts`, export:

```ts
export function listAuctions(token: string): Promise<Auction[]> {
  return request("/api/auctions", {}, token);
}

export function getSnapshot(token: string, id: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/snapshot`, {}, token);
}

export function placeBid(token: string, id: string, amount: number): Promise<BidResult> {
  return request(`/api/auctions/${id}/bids`, {
    method: "POST",
    body: JSON.stringify({ requestId: crypto.randomUUID(), amount })
  }, token);
}

export function openAuctionSocket(id: string, token: string): WebSocket {
  return new WebSocket(`${WS_BASE}/ws/auctions/${id}?token=${encodeURIComponent(token)}`);
}
```

- [ ] **Step 2: Implement mobile app**

Create `web/src/MobileApp.tsx`:

```tsx
import { useEffect, useMemo, useState } from "react";
import { Auction, getSnapshot, listAuctions, openAuctionSocket, placeBid, Snapshot } from "./api";
import type { Session } from "./session";

export function MobileApp({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const [items, setItems] = useState<Auction[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [amount, setAmount] = useState(100);
  const [message, setMessage] = useState("");
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    void listAuctions(session.token).then((next) => {
      setItems(next);
      if (next[0]) setSelectedId(next[0].id);
    });
  }, [session.token]);

  useEffect(() => {
    if (!selectedId) return;
    let closed = false;
    let socket: WebSocket | null = null;

    async function connect() {
      try {
        const next = await getSnapshot(session.token, selectedId);
        setSnapshot(next);
        setAmount(next.nextMinimumBid);
      } catch (err) {
        setMessage(err instanceof Error ? err.message : "恢复状态失败");
      }
      socket = openAuctionSocket(selectedId, session.token);
      socket.onmessage = (event) => {
        const payload = JSON.parse(event.data);
        const next = payload.snapshot ?? payload.Snapshot;
        if (next) {
          setSnapshot(next);
          setAmount(next.nextMinimumBid);
        }
      };
      socket.onclose = () => {
        if (!closed) {
          setMessage("连接中断，正在恢复");
          window.setTimeout(connect, 800);
        }
      };
    }

    void connect();
    return () => {
      closed = true;
      socket?.close();
    };
  }, [selectedId, session.token]);

  const selected = items.find((item) => item.id === selectedId);
  const remaining = useMemo(() => formatRemaining(snapshot?.endsAt, now), [snapshot?.endsAt, now]);

  async function bid() {
    if (!selectedId) return;
    setMessage("出价提交中...");
    try {
      const result = await placeBid(session.token, selectedId, amount);
      setSnapshot(result.snapshot);
      setAmount(result.snapshot.nextMinimumBid);
      setMessage(result.snapshot.status === "SOLD" ? "竞拍成交" : "出价成功");
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "出价失败");
    }
  }

  return (
    <main className="mobile-shell">
      <header className="mobile-header"><strong>{session.user.name}</strong><button onClick={onLogout}>退出</button></header>
      <select value={selectedId} onChange={(e) => setSelectedId(e.target.value)}>
        {items.map((item) => <option key={item.id} value={item.id}>{item.product.name}</option>)}
      </select>
      <section className="product-hero">
        <img src={selected?.product.imageUrl} alt={selected?.product.name} />
        <h1>{selected?.product.name ?? "暂无竞拍"}</h1>
        <p>{selected?.product.description}</p>
      </section>
      <section className="price-panel">
        <span>当前价</span>
        <strong>¥{snapshot?.currentPrice ?? selected?.currentPrice ?? 0}</strong>
        <em>{snapshot?.status ?? selected?.status ?? "-"}</em>
      </section>
      <section className="mobile-metrics">
        <Metric label="倒计时" value={remaining} />
        <Metric label="最低出价" value={`¥${snapshot?.nextMinimumBid ?? amount}`} />
        <Metric label="我的排名" value={snapshot?.rank ? `#${snapshot.rank}` : "-"} />
        <Metric label="参与人数" value={String(snapshot?.participants ?? 0)} />
      </section>
      {message && <div className="mobile-message">{message}</div>}
      <footer className="bid-footer">
        <input type="number" value={amount} onChange={(e) => setAmount(Number(e.target.value))} />
        <button disabled={snapshot?.status !== "RUNNING"} onClick={() => void bid()}>立即出价</button>
      </footer>
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return <div><span>{label}</span><b>{value}</b></div>;
}

function formatRemaining(endsAt: string | undefined, now: number) {
  if (!endsAt) return "-";
  const ms = Math.max(0, new Date(endsAt).getTime() - now);
  const total = Math.ceil(ms / 1000);
  const min = Math.floor(total / 60).toString().padStart(2, "0");
  const sec = (total % 60).toString().padStart(2, "0");
  return `${min}:${sec}`;
}
```

- [ ] **Step 3: Replace styles**

Replace `web/src/styles.css` with styles that define `.login-shell`, `.admin-shell`, `.admin-list`, `.admin-workspace`, `.panel`, `.mobile-shell`, `.product-hero`, `.price-panel`, `.mobile-metrics`, and `.bid-footer`. Keep responsive constraints:

```css
:root {
  color: #20201d;
  background: #f5f2eb;
  font-family: "Microsoft YaHei", "PingFang SC", "Segoe UI", sans-serif;
  --ink: #20201d;
  --muted: #7a7468;
  --line: #ddd6c9;
  --paper: #fffdf8;
  --accent: #1d6f5c;
  --danger: #c94f3b;
  --gold: #c59138;
}
* { box-sizing: border-box; }
body { margin: 0; min-width: 320px; min-height: 100vh; }
button, input, textarea, select { font: inherit; }
button { border: 1px solid var(--accent); border-radius: 6px; background: var(--accent); color: #fff; min-height: 40px; padding: 0 14px; cursor: pointer; }
button:disabled { opacity: .45; cursor: not-allowed; }
input, textarea, select { width: 100%; border: 1px solid var(--line); border-radius: 6px; background: #fff; padding: 10px; color: var(--ink); }
textarea { min-height: 84px; resize: vertical; }
.login-shell { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
.login-panel { width: min(420px, 100%); background: var(--paper); border: 1px solid var(--line); border-radius: 8px; padding: 24px; display: grid; gap: 16px; }
.eyebrow { margin: 0; color: var(--accent); font-size: 12px; }
.segmented { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.segmented button { background: #fff; color: var(--ink); border-color: var(--line); }
.segmented .active { background: var(--accent); color: #fff; border-color: var(--accent); }
.error, .mobile-message, .notice { border: 1px solid var(--line); background: #fff7e6; color: #664100; border-radius: 6px; padding: 10px; }
.admin-shell { min-height: 100vh; display: grid; grid-template-columns: 300px minmax(0, 1fr); }
.admin-list { border-right: 1px solid var(--line); background: #ebe5d9; padding: 16px; display: grid; align-content: start; gap: 10px; }
.bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.row { background: #fff; color: var(--ink); border-color: var(--line); display: flex; justify-content: space-between; }
.row.active { border-color: var(--accent); box-shadow: inset 4px 0 0 var(--accent); }
.admin-workspace { padding: 24px; }
.admin-grid { display: grid; grid-template-columns: minmax(340px, 1fr) minmax(320px, .8fr); gap: 16px; align-items: start; }
.panel { background: var(--paper); border: 1px solid var(--line); border-radius: 8px; padding: 16px; display: grid; gap: 12px; }
.number-grid, .mobile-metrics { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.mobile-shell { max-width: 440px; min-height: 100vh; margin: 0 auto; background: var(--paper); padding: 14px 14px 88px; display: grid; gap: 12px; }
.mobile-header { display: flex; align-items: center; justify-content: space-between; }
.product-hero img { width: 100%; aspect-ratio: 4 / 3; object-fit: cover; border-radius: 8px; }
.product-hero h1 { margin: 12px 0 6px; font-size: 24px; }
.product-hero p { margin: 0; color: var(--muted); }
.price-panel { border: 1px solid var(--line); border-radius: 8px; padding: 16px; display: grid; gap: 4px; }
.price-panel strong { color: var(--gold); font-size: 36px; }
.mobile-metrics > div { border: 1px solid var(--line); border-radius: 8px; padding: 12px; background: #fff; display: grid; gap: 4px; }
.mobile-metrics span { color: var(--muted); font-size: 12px; }
.bid-footer { position: fixed; left: 50%; bottom: 0; transform: translateX(-50%); width: min(440px, 100%); background: #fff; border-top: 1px solid var(--line); padding: 12px; display: grid; grid-template-columns: 1fr 132px; gap: 10px; }
@media (max-width: 760px) {
  .admin-shell { grid-template-columns: 1fr; }
  .admin-list { border-right: 0; border-bottom: 1px solid var(--line); }
  .admin-grid { grid-template-columns: 1fr; }
}
```

- [ ] **Step 4: Build and commit frontend split**

Run:

```powershell
cd web
npm run build
```

Expected: PASS.

Commit:

```powershell
git add web/src/api.ts web/src/session.ts web/src/App.tsx web/src/LoginPage.tsx web/src/AdminApp.tsx web/src/MobileApp.tsx web/src/styles.css web/src/main.tsx
git commit -m "feat: split admin and mobile auction clients"
```

## Task 11: End-to-End Test and Local Data Hygiene

**Files:**
- Modify: `web/tests/realtime-ranking.spec.ts`
- Modify: `web/playwright.config.ts`
- Modify: `.gitignore`

- [ ] **Step 1: Ignore generated local data**

Add to `.gitignore`:

```gitignore
.superpowers/
data/
web/test-results/
.backend*.log
.frontend*.log
```

- [ ] **Step 2: Update Playwright test**

Replace `web/tests/realtime-ranking.spec.ts` with:

```ts
import { expect, test } from "@playwright/test";

test("admin starts auction and mobile bidder wins at ceiling price", async ({ browser }) => {
  const admin = await browser.newPage();
  await admin.goto("/admin");
  await admin.getByLabel("昵称").fill("管理员");
  await admin.getByRole("button", { name: "管理端" }).click();
  await admin.getByRole("button", { name: "进入系统" }).click();
  await expect(admin.getByRole("heading", { name: "竞拍控制台" })).toBeVisible();
  await admin.getByRole("button", { name: "创建竞拍" }).click();
  await expect(admin.getByText("创建竞拍成功")).toBeVisible();
  await admin.getByRole("button", { name: "启动" }).click();
  await expect(admin.getByText("启动竞拍成功")).toBeVisible();

  const bidder = await browser.newPage();
  await bidder.goto("/m");
  await bidder.getByLabel("昵称").fill("用户A");
  await bidder.getByRole("button", { name: "用户端" }).click();
  await bidder.getByRole("button", { name: "进入系统" }).click();
  await expect(bidder.getByText("当前价")).toBeVisible();
  await bidder.locator(".bid-footer input").fill("3000");
  await bidder.getByRole("button", { name: "立即出价" }).click();
  await expect(bidder.getByText("竞拍成交")).toBeVisible();
  await expect(bidder.getByText("SOLD")).toBeVisible();
});
```

- [ ] **Step 3: Run backend and frontend tests**

Run backend:

```powershell
go test ./internal/...
```

Expected: PASS.

Run frontend build:

```powershell
cd web
npm run build
```

Expected: PASS.

Run e2e:

```powershell
cd web
npm run test:e2e
```

Expected: PASS. If Playwright browsers are missing, run `cmd /c npx playwright install chromium` and rerun.

- [ ] **Step 4: Commit**

```powershell
git add .gitignore web/tests/realtime-ranking.spec.ts web/playwright.config.ts
git commit -m "test: cover realtime auction core flow"
```

## Task 12: Final Verification

**Files:**
- Modify only if verification reveals an implementation bug.

- [ ] **Step 1: Run full Go tests**

Run:

```powershell
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run:

```powershell
cd web
npm run build
```

Expected: PASS.

- [ ] **Step 3: Run e2e test**

Run:

```powershell
cd web
npm run test:e2e
```

Expected: PASS.

- [ ] **Step 4: Manual smoke**

Start backend:

```powershell
go run ./cmd/api
```

Start frontend in a second shell:

```powershell
cd web
npm run dev
```

Open:

```text
http://localhost:5173/admin
http://localhost:5173/m
```

Expected:

- Admin can log in, create, start, and cancel a draft/running auction.
- Mobile bidder can log in, see current auction, bid while `RUNNING`, and see `SOLD` after ceiling price.
- Admin does not show a normal manual finish button.
- Refreshing mobile page restores snapshot state.

- [ ] **Step 5: Final commit if fixes were needed**

If Step 1-4 required any fixes:

```powershell
git add <changed-files>
git commit -m "fix: stabilize realtime auction refactor"
```

If no fixes were needed, do not create an empty commit.

## Self-Review

- Spec coverage: state machine, file persistence, demo login, admin console, mobile H5 bidder flow, structured errors, WebSocket recovery, and tests are mapped to Tasks 1-12.
- Placeholder scan: no forbidden placeholder terms or unspecified deferred steps remain.
- Type consistency: `auction.Role`, `auction.User`, `auction.Session`, `service.LoginSession`, and frontend `Session` are defined before use in later tasks.
