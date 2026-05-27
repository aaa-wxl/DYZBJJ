# Redis Auction Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将运行时实时竞拍状态从进程内 `MemoryStore` 迁移到真实 Redis，并用 Docker Compose 提供 Redis 与预留 MySQL。

**Architecture:** 后端继续使用现有 Go 标准库 HTTP、`AuctionService`、文件 repository 和 WebSocket Hub。新增 `internal/redis.RedisStore` 作为 `redis.Store` 的生产实现，使用 Redis Hash/ZSet/String 和 Lua 脚本完成原子出价、排名、幂等、取消和到期结算；`MemoryStore` 保留给单元测试。

**Tech Stack:** Go 1.22, `github.com/redis/go-redis/v9`, Redis 7, Docker Compose, MySQL 8 预留, React/Vite, Playwright。

---

## 文件结构

- Create: `docker-compose.yml`
  - 本地依赖编排，包含 `redis` 和 `mysql`。
- Modify: `README.md`
  - 增加后端、前端、Docker 依赖启动方式。
- Modify: `web/start.md`
  - 增加前端调试时需要先启动后端和 Redis 的说明。
- Modify: `go.mod`
  - 增加 `github.com/redis/go-redis/v9`。
- Modify: `internal/config/config.go`
  - 增加 Redis password、DB、required 开关；保留 `DATABASE_URL` 作为 MySQL 后续接入配置。
- Modify: `internal/redis/keys.go`
  - 明确 Redis key 命名：snapshot、ranking、amounts、rank_seq、seq、request、events。
- Create: `internal/redis/redis_store.go`
  - 真实 Redis Store 的构造、序列化、Snapshot、InitAuction。
- Create: `internal/redis/scripts.go`
  - 放置出价、取消、到期结算 Lua 脚本字符串。
- Create: `internal/redis/redis_store_test.go`
  - 需要 Docker Redis 的集成测试；未设置 `REDIS_INTEGRATION=1` 时跳过。
- Modify: `internal/redis/store_test.go`
  - 保留 MemoryStore 单元测试；修正并发 requestId 生成方式。
- Modify: `cmd/api/main.go`
  - 启动时连接 Redis，`PING` 成功后使用 `RedisStore`，失败则退出。
- Modify: `web/playwright.config.ts`
  - E2E 后端命令增加 `REDIS_ADDR`；E2E 运行前要求 Docker Redis 已启动。

## 实施约束

- 中文文档和简洁中文注释优先。
- 不把用户、商品、竞拍配置、出价流水、订单迁到 MySQL。
- 不静默回退到 `MemoryStore`。生产启动 Redis 失败必须报错退出。
- 每个任务完成后运行该任务对应测试并提交一次。

---

### Task 1: Docker Compose 与启动文档

**Files:**
- Create: `docker-compose.yml`
- Modify: `README.md`
- Modify: `web/start.md`

- [ ] **Step 1: 添加 Docker Compose 文件**

Create `docker-compose.yml`:

```yaml
services:
  redis:
    image: redis:7-alpine
    container_name: realtime-auction-redis
    command: ["redis-server", "--appendonly", "yes"]
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  mysql:
    image: mysql:8.0
    container_name: realtime-auction-mysql
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: auction
      MYSQL_USER: auction
      MYSQL_PASSWORD: auction
    ports:
      - "3306:3306"
    volumes:
      - mysql-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1", "-uauction", "-pauction"]
      interval: 10s
      timeout: 5s
      retries: 10

volumes:
  redis-data:
  mysql-data:
```

- [ ] **Step 2: 验证 Compose 配置**

Run:

```powershell
docker compose config
```

Expected:

```text
services:
  mysql:
  redis:
volumes:
  mysql-data:
  redis-data:
```

如果 Docker 仍在安装中，记录为“本机暂未安装 Docker”，继续写代码，最后统一验证。

- [ ] **Step 3: 更新 README 启动说明**

在 `README.md` 增加本地启动段落：

```markdown
## 本地启动

先启动依赖：

```powershell
docker compose up -d redis mysql
```

启动后端：

```powershell
$env:REDIS_ADDR="127.0.0.1:6379"
$env:HTTP_ADDR="127.0.0.1:8080"
go run ./cmd/api
```

启动前端：

```powershell
cd web
$env:VITE_API_BASE="http://127.0.0.1:8080"
npm run dev -- --host 127.0.0.1 --port 5173
```

当前阶段 Redis 承载实时竞拍状态；MySQL 仅在 Docker Compose 中预留，业务数据仍写入 `data/*.json`。
```

- [ ] **Step 4: 更新 `web/start.md`**

追加：

```markdown
## 依赖顺序

1. `docker compose up -d redis mysql`
2. 后端 `go run ./cmd/api`
3. 前端 `npm run dev`

如果登录或出价接口返回网络错误，先检查后端是否已启动；如果后端启动失败，先检查 Redis 容器是否健康。
```

- [ ] **Step 5: 提交**

Run:

```powershell
git add docker-compose.yml README.md web/start.md
git commit -m "docs: add docker startup guide"
```

Expected: commit succeeds.

---

### Task 2: Redis 依赖与配置

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/config/config.go`

- [ ] **Step 1: 增加 Redis Go client**

Run:

```powershell
go get github.com/redis/go-redis/v9
```

Expected:

```text
go: added github.com/redis/go-redis/v9
```

- [ ] **Step 2: 为配置写测试**

Create or modify `internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadRedisConfigDefaults(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("REDIS_REQUIRED", "")

	cfg := Load()

	if cfg.RedisAddr != "127.0.0.1:6379" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "" {
		t.Fatalf("RedisPassword = %q", cfg.RedisPassword)
	}
	if cfg.RedisDB != 0 {
		t.Fatalf("RedisDB = %d", cfg.RedisDB)
	}
	if !cfg.RedisRequired {
		t.Fatal("RedisRequired should default to true")
	}
}

func TestLoadRedisConfigFromEnv(t *testing.T) {
	t.Setenv("REDIS_ADDR", "redis.internal:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("REDIS_REQUIRED", "false")

	cfg := Load()

	if cfg.RedisAddr != "redis.internal:6380" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "secret" {
		t.Fatalf("RedisPassword = %q", cfg.RedisPassword)
	}
	if cfg.RedisDB != 2 {
		t.Fatalf("RedisDB = %d", cfg.RedisDB)
	}
	if cfg.RedisRequired {
		t.Fatal("RedisRequired should be false")
	}
}
```

- [ ] **Step 3: 运行配置测试，确认失败**

Run:

```powershell
go test -count=1 ./internal/config
```

Expected: FAIL，提示 `RedisPassword`、`RedisDB` 或 `RedisRequired` 未定义。

- [ ] **Step 4: 实现配置字段**

Replace `internal/config/config.go` with:

```go
// config 负责读取本地运行配置，并为缺省环境提供可启动的默认值。
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisRequired bool
	FrontendURL   string
	JWTSecret     string
	JWTTTL        time.Duration
}

// Load 从环境变量加载配置，缺省值用于本地演示。
func Load() Config {
	return Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		DatabaseURL:   env("DATABASE_URL", "mysql://auction:auction@tcp(127.0.0.1:3306)/auction"),
		RedisAddr:     env("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: env("REDIS_PASSWORD", ""),
		RedisDB:       envInt("REDIS_DB", 0),
		RedisRequired: envBool("REDIS_REQUIRED", true),
		FrontendURL:   env("FRONTEND_URL", "http://localhost:5173"),
		JWTSecret:     env("JWT_SECRET", "local-demo-jwt-secret"),
		JWTTTL:        24 * time.Hour,
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
```

- [ ] **Step 5: 运行配置测试，确认通过**

Run:

```powershell
go test -count=1 ./internal/config
```

Expected: PASS.

- [ ] **Step 6: 提交**

Run:

```powershell
git add go.mod go.sum internal/config/config.go internal/config/config_test.go
git commit -m "feat: add redis runtime config"
```

Expected: commit succeeds.

---

### Task 3: Redis Key 命名与单元测试

**Files:**
- Modify: `internal/redis/keys.go`
- Create: `internal/redis/keys_test.go`

- [ ] **Step 1: 写 key 测试**

Create `internal/redis/keys_test.go`:

```go
package redis

import "testing"

func TestAuctionKeys(t *testing.T) {
	auctionID := "auc-1"

	tests := map[string]string{
		AuctionSnapshotKey(auctionID):       "auction:auc-1:snapshot",
		AuctionRankKey(auctionID):           "auction:auc-1:ranking",
		AuctionAmountKey(auctionID):         "auction:auc-1:amounts",
		AuctionRankSeqKey(auctionID):        "auction:auc-1:rank_seq",
		AuctionSeqKey(auctionID):            "auction:auc-1:seq",
		AuctionRequestKey(auctionID, "r-1"): "auction:auc-1:request:r-1",
		AuctionEventsKey(auctionID):         "auction:auc-1:events",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}
```

- [ ] **Step 2: 运行 key 测试，确认失败**

Run:

```powershell
go test -count=1 ./internal/redis -run TestAuctionKeys
```

Expected: FAIL，提示 `AuctionAmountKey`、`AuctionRankSeqKey`、`AuctionSeqKey`、`AuctionEventsKey` 未定义，或旧 key 值不匹配。

- [ ] **Step 3: 实现 key 命名**

Replace `internal/redis/keys.go` with:

```go
// redis 集中维护竞拍实时状态在 Redis 中的 key 命名约定。
package redis

import "fmt"

func AuctionSnapshotKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:snapshot", auctionID)
}

func AuctionRankKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:ranking", auctionID)
}

func AuctionAmountKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:amounts", auctionID)
}

func AuctionRankSeqKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:rank_seq", auctionID)
}

func AuctionSeqKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:seq", auctionID)
}

func AuctionRequestKey(auctionID, requestID string) string {
	return fmt.Sprintf("auction:%s:request:%s", auctionID, requestID)
}

func AuctionEventsKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:events", auctionID)
}

// AuctionRoomKey 兼容旧的房间命名调用；新事件通道使用 AuctionEventsKey。
func AuctionRoomKey(auctionID string) string {
	return AuctionEventsKey(auctionID)
}
```

- [ ] **Step 4: 运行 Redis 包测试**

Run:

```powershell
go test -count=1 ./internal/redis
```

Expected: PASS.

- [ ] **Step 5: 提交**

Run:

```powershell
git add internal/redis/keys.go internal/redis/keys_test.go
git commit -m "feat: define redis auction keys"
```

Expected: commit succeeds.

---

### Task 4: RedisStore Snapshot 与初始化

**Files:**
- Create: `internal/redis/redis_store.go`
- Create: `internal/redis/redis_store_test.go`

- [ ] **Step 1: 写 Redis 集成测试辅助函数**

Create `internal/redis/redis_store_test.go`:

```go
package redis

import (
	"context"
	"os"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/domain/auction"
)

func newRedisStoreForTest(t *testing.T) (*RedisStore, *goredis.Client) {
	t.Helper()
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run Redis integration tests")
	}
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := goredis.NewClient(&goredis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return NewRedisStore(client), client
}

func startedSnapshot(now time.Time) auction.Snapshot {
	return auction.Snapshot{
		AuctionID:      "auction-redis-1",
		Product:        auction.Product{Name: "星河翡翠手镯", ImageURL: "/demo.jpg", Description: "demo"},
		Status:         auction.StatusRunning,
		CurrentPrice:   0,
		EndsAt:         now.Add(time.Minute),
		ServerTime:     now,
		NextMinimumBid: 100,
		Rules: auction.Rules{
			StartPrice:      0,
			Increment:       100,
			Duration:        time.Minute,
			CeilingPrice:    10_000,
			ExtendThreshold: 20 * time.Second,
			ExtendBy:        30 * time.Second,
		},
	}
}

func TestRedisStoreInitAndSnapshot(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()

	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatalf("init auction: %v", err)
	}
	snapshot, err := store.Snapshot("auction-redis-1", "user-a")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snapshot.AuctionID != "auction-redis-1" {
		t.Fatalf("AuctionID = %q", snapshot.AuctionID)
	}
	if snapshot.Product.Name != "星河翡翠手镯" {
		t.Fatalf("Product.Name = %q", snapshot.Product.Name)
	}
	if snapshot.CurrentPrice != 0 {
		t.Fatalf("CurrentPrice = %d", snapshot.CurrentPrice)
	}
	if snapshot.NextMinimumBid != 100 {
		t.Fatalf("NextMinimumBid = %d", snapshot.NextMinimumBid)
	}
}
```

- [ ] **Step 2: 运行 Redis 集成测试，确认失败**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run TestRedisStoreInitAndSnapshot
```

Expected: FAIL，提示 `RedisStore` 或 `NewRedisStore` 未定义。

- [ ] **Step 3: 实现 RedisStore 初始化和 Snapshot**

Create `internal/redis/redis_store.go`:

```go
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/domain/auction"
)

const (
	requestTTL = 10 * time.Minute
	stateTTL   = 24 * time.Hour
)

type RedisStore struct {
	client *goredis.Client
}

func NewRedisStore(client *goredis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) InitAuction(snapshot auction.Snapshot) error {
	if snapshot.AuctionID == "" {
		return fmt.Errorf("auction id is required")
	}
	if err := snapshot.Rules.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if snapshot.ServerTime.IsZero() {
		snapshot.ServerTime = now
	}
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	productJSON, err := json.Marshal(snapshot.Product)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, AuctionSnapshotKey(snapshot.AuctionID), map[string]any{
		"auctionId":         snapshot.AuctionID,
		"productJson":       string(productJSON),
		"status":            string(snapshot.Status),
		"currentPrice":      snapshot.CurrentPrice,
		"highestBidder":     snapshot.HighestBidder,
		"endsAtUnixMs":      snapshot.EndsAt.UnixMilli(),
		"serverTimeUnixMs":  snapshot.ServerTime.UnixMilli(),
		"startPrice":        snapshot.Rules.StartPrice,
		"increment":         snapshot.Rules.Increment,
		"durationMs":        snapshot.Rules.Duration.Milliseconds(),
		"ceilingPrice":      snapshot.Rules.CeilingPrice,
		"extendThresholdMs": snapshot.Rules.ExtendThreshold.Milliseconds(),
		"extendByMs":        snapshot.Rules.ExtendBy.Milliseconds(),
	})
	pipe.Del(ctx, AuctionRankKey(snapshot.AuctionID), AuctionAmountKey(snapshot.AuctionID), AuctionRankSeqKey(snapshot.AuctionID), AuctionSeqKey(snapshot.AuctionID))
	pipe.Expire(ctx, AuctionSnapshotKey(snapshot.AuctionID), stateTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) Snapshot(auctionID, userID string) (auction.Snapshot, error) {
	ctx := context.Background()
	snapshot, err := s.loadSnapshot(ctx, auctionID)
	if err != nil {
		return auction.Snapshot{}, err
	}
	snapshot.ServerTime = time.Now().UTC()
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	if err := s.fillRanking(ctx, &snapshot, userID); err != nil {
		return auction.Snapshot{}, err
	}
	return snapshot, nil
}

func (s *RedisStore) loadSnapshot(ctx context.Context, auctionID string) (auction.Snapshot, error) {
	values, err := s.client.HGetAll(ctx, AuctionSnapshotKey(auctionID)).Result()
	if err != nil {
		return auction.Snapshot{}, err
	}
	if len(values) == 0 {
		return auction.Snapshot{}, ErrAuctionNotFound
	}
	product := auction.Product{}
	if err := json.Unmarshal([]byte(values["productJson"]), &product); err != nil {
		return auction.Snapshot{}, err
	}
	currentPrice, err := parseInt(values, "currentPrice")
	if err != nil {
		return auction.Snapshot{}, err
	}
	endsAtMs, err := parseInt(values, "endsAtUnixMs")
	if err != nil {
		return auction.Snapshot{}, err
	}
	serverTimeMs, err := parseInt(values, "serverTimeUnixMs")
	if err != nil {
		return auction.Snapshot{}, err
	}
	rules, err := parseRules(values)
	if err != nil {
		return auction.Snapshot{}, err
	}
	return auction.Snapshot{
		AuctionID:     values["auctionId"],
		Product:       product,
		Rules:         rules,
		Status:        auction.Status(values["status"]),
		CurrentPrice:  currentPrice,
		HighestBidder: values["highestBidder"],
		EndsAt:        time.UnixMilli(endsAtMs).UTC(),
		ServerTime:    time.UnixMilli(serverTimeMs).UTC(),
	}, nil
}

func (s *RedisStore) fillRanking(ctx context.Context, snapshot *auction.Snapshot, userID string) error {
	rows, err := s.client.ZRevRangeWithScores(ctx, AuctionRankKey(snapshot.AuctionID), 0, 4).Result()
	if err != nil {
		return err
	}
	snapshot.Leaderboard = snapshot.Leaderboard[:0]
	for i, row := range rows {
		amount, err := s.client.HGet(ctx, AuctionAmountKey(snapshot.AuctionID), row.Member.(string)).Int64()
		if err != nil {
			return err
		}
		snapshot.Leaderboard = append(snapshot.Leaderboard, auction.LeaderboardEntry{
			Rank:   i + 1,
			UserID: row.Member.(string),
			Amount: amount,
		})
	}
	count, err := s.client.ZCard(ctx, AuctionRankKey(snapshot.AuctionID)).Result()
	if err != nil {
		return err
	}
	snapshot.Participants = int(count)
	snapshot.Rank = 0
	if userID != "" {
		rank, err := s.client.ZRevRank(ctx, AuctionRankKey(snapshot.AuctionID), userID).Result()
		if err == nil {
			snapshot.Rank = int(rank) + 1
		} else if err != goredis.Nil {
			return err
		}
	}
	return nil
}

func parseRules(values map[string]string) (auction.Rules, error) {
	startPrice, err := parseInt(values, "startPrice")
	if err != nil {
		return auction.Rules{}, err
	}
	increment, err := parseInt(values, "increment")
	if err != nil {
		return auction.Rules{}, err
	}
	durationMs, err := parseInt(values, "durationMs")
	if err != nil {
		return auction.Rules{}, err
	}
	ceilingPrice, err := parseInt(values, "ceilingPrice")
	if err != nil {
		return auction.Rules{}, err
	}
	extendThresholdMs, err := parseInt(values, "extendThresholdMs")
	if err != nil {
		return auction.Rules{}, err
	}
	extendByMs, err := parseInt(values, "extendByMs")
	if err != nil {
		return auction.Rules{}, err
	}
	return auction.Rules{
		StartPrice:      startPrice,
		Increment:       increment,
		Duration:        time.Duration(durationMs) * time.Millisecond,
		CeilingPrice:    ceilingPrice,
		ExtendThreshold: time.Duration(extendThresholdMs) * time.Millisecond,
		ExtendBy:        time.Duration(extendByMs) * time.Millisecond,
	}, nil
}

func parseInt(values map[string]string, key string) (int64, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("missing snapshot field %s", key)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot field %s: %w", key, err)
	}
	return parsed, nil
}
```

- [ ] **Step 4: 运行 Redis 初始化测试**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run TestRedisStoreInitAndSnapshot
```

Expected: PASS.

- [ ] **Step 5: 提交**

Run:

```powershell
git add internal/redis/redis_store.go internal/redis/redis_store_test.go
git commit -m "feat: add redis store snapshot"
```

Expected: commit succeeds.

---

### Task 5: Redis Lua 原子出价、排名和幂等

**Files:**
- Create: `internal/redis/scripts.go`
- Modify: `internal/redis/redis_store.go`
- Modify: `internal/redis/redis_store_test.go`
- Modify: `internal/redis/store_test.go`

- [ ] **Step 1: 修正 MemoryStore 并发测试 requestId**

In `internal/redis/store_test.go`, replace:

```go
RequestID: "req-concurrent-" + string(rune('a'+i)),
```

with:

```go
RequestID: fmt.Sprintf("req-concurrent-%d", i),
```

- [ ] **Step 2: 添加 Redis 出价集成测试**

Append to `internal/redis/redis_store_test.go`:

```go
func TestRedisStorePlaceBidUpdatesSnapshotLeaderboardAndRank(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-redis-1",
		UserID:    "user-a",
		UserName:  "用户A",
		RequestID: "req-a-1",
		Amount:    100,
		Now:       now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("place bid: %v", err)
	}

	if result.Snapshot.CurrentPrice != 100 {
		t.Fatalf("CurrentPrice = %d", result.Snapshot.CurrentPrice)
	}
	if result.Snapshot.HighestBidder != "user-a" {
		t.Fatalf("HighestBidder = %q", result.Snapshot.HighestBidder)
	}
	if result.Snapshot.Rank != 1 {
		t.Fatalf("Rank = %d", result.Snapshot.Rank)
	}
	if len(result.Snapshot.Leaderboard) != 1 || result.Snapshot.Leaderboard[0].Amount != 100 {
		t.Fatalf("Leaderboard = %+v", result.Snapshot.Leaderboard)
	}
}

func TestRedisStorePlaceBidIsIdempotent(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}

	first, err := store.PlaceBid(BidCommand{AuctionID: "auction-redis-1", UserID: "user-a", RequestID: "same-req", Amount: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PlaceBid(BidCommand{AuctionID: "auction-redis-1", UserID: "user-a", RequestID: "same-req", Amount: 100, Now: now})
	if err != nil {
		t.Fatal(err)
	}

	if !second.Idempotent {
		t.Fatal("second bid should be idempotent")
	}
	if first.BidID != second.BidID {
		t.Fatalf("BidID changed: %s vs %s", first.BidID, second.BidID)
	}
}

func TestRedisStoreConcurrentBidsNeverDecreasePrice(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(startedSnapshot(now)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = store.PlaceBid(BidCommand{
				AuctionID: "auction-redis-1",
				UserID:    fmt.Sprintf("user-%02d", i),
				RequestID: fmt.Sprintf("req-%02d", i),
				Amount:    int64(i * 100),
				Now:       now.Add(time.Duration(i) * time.Millisecond),
			})
		}()
	}
	wg.Wait()

	snapshot, err := store.Snapshot("auction-redis-1", "user-20")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentPrice != 2000 {
		t.Fatalf("CurrentPrice = %d, want 2000", snapshot.CurrentPrice)
	}
	if snapshot.Rank != 1 {
		t.Fatalf("Rank = %d, want 1", snapshot.Rank)
	}
}

func TestRedisStoreCeilingPriceWinsOverExtension(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	snapshot := startedSnapshot(now)
	snapshot.EndsAt = now.Add(5 * time.Second)
	snapshot.Rules.CeilingPrice = 1000
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}

	result, err := store.PlaceBid(BidCommand{
		AuctionID: "auction-redis-1",
		UserID:    "user-a",
		RequestID: "req-ceiling",
		Amount:    1000,
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Snapshot.Status != auction.StatusSold {
		t.Fatalf("Status = %s", result.Snapshot.Status)
	}
	if result.Extended {
		t.Fatal("ceiling bid should not extend auction")
	}
}
```

Ensure imports in `redis_store_test.go` include:

```go
	"fmt"
	"sync"
```

- [ ] **Step 3: 运行 Redis 出价测试，确认失败**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run "TestRedisStore(PlaceBid|Concurrent|Ceiling)"
```

Expected: FAIL，提示 `RedisStore.PlaceBid` 未实现。

- [ ] **Step 4: 添加 Lua 脚本**

Create `internal/redis/scripts.go`:

```go
package redis

const placeBidScript = `
local snapshotKey = KEYS[1]
local rankKey = KEYS[2]
local amountKey = KEYS[3]
local rankSeqKey = KEYS[4]
local seqKey = KEYS[5]
local requestKey = KEYS[6]

local auctionID = ARGV[1]
local userID = ARGV[2]
local requestID = ARGV[3]
local amount = tonumber(ARGV[4])
local nowMs = tonumber(ARGV[5])
local bidID = ARGV[6]
local requestTTLSeconds = tonumber(ARGV[7])

local existing = redis.call("GET", requestKey)
if existing then
  return existing
end

if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end

local status = redis.call("HGET", snapshotKey, "status")
local currentPrice = tonumber(redis.call("HGET", snapshotKey, "currentPrice"))
local endsAtMs = tonumber(redis.call("HGET", snapshotKey, "endsAtUnixMs"))
local startPrice = tonumber(redis.call("HGET", snapshotKey, "startPrice"))
local increment = tonumber(redis.call("HGET", snapshotKey, "increment"))
local ceilingPrice = tonumber(redis.call("HGET", snapshotKey, "ceilingPrice"))
local extendThresholdMs = tonumber(redis.call("HGET", snapshotKey, "extendThresholdMs"))
local extendByMs = tonumber(redis.call("HGET", snapshotKey, "extendByMs"))

if status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status, nextMinimum=currentPrice + increment})
end
if nowMs > endsAtMs then
  return cjson.encode({ok=false, error="expired", status=status, nextMinimum=currentPrice + increment})
end

local minimum = currentPrice + increment
if minimum < startPrice then
  minimum = startPrice
end
if amount < minimum then
  return cjson.encode({ok=false, error="low_bid", nextMinimum=minimum})
end
if ((amount - startPrice) % increment) ~= 0 then
  return cjson.encode({ok=false, error="increment", nextMinimum=minimum})
end

local seq = tonumber(redis.call("INCR", seqKey))
local previousAmount = tonumber(redis.call("HGET", amountKey, userID) or "-1")
if amount > previousAmount then
  redis.call("HSET", amountKey, userID, amount)
  redis.call("HSET", rankSeqKey, userID, seq)
  redis.call("ZADD", rankKey, amount * 1000000000 - seq, userID)
end

local extended = false
local newStatus = status
if amount >= ceilingPrice then
  newStatus = "SOLD"
elseif endsAtMs - nowMs <= extendThresholdMs and extendByMs > 0 then
  endsAtMs = endsAtMs + extendByMs
  extended = true
end

redis.call("HSET", snapshotKey,
  "status", newStatus,
  "currentPrice", amount,
  "highestBidder", userID,
  "endsAtUnixMs", endsAtMs,
  "serverTimeUnixMs", nowMs
)

local result = cjson.encode({
  ok=true,
  bidId=bidID,
  idempotent=false,
  extended=extended,
  status=newStatus,
  currentPrice=amount,
  highestBidder=userID,
  endsAtUnixMs=endsAtMs,
  serverTimeUnixMs=nowMs,
  nextMinimum=amount + increment
})
redis.call("SET", requestKey, result, "EX", requestTTLSeconds)
return result
`
```

- [ ] **Step 5: 实现 `RedisStore.PlaceBid`**

Append to `internal/redis/redis_store.go`:

```go
type bidScriptResult struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error"`
	BidID            string         `json:"bidId"`
	Idempotent       bool           `json:"idempotent"`
	Extended         bool           `json:"extended"`
	Status           auction.Status `json:"status"`
	CurrentPrice     int64          `json:"currentPrice"`
	HighestBidder    string         `json:"highestBidder"`
	EndsAtUnixMs     int64          `json:"endsAtUnixMs"`
	ServerTimeUnixMs int64          `json:"serverTimeUnixMs"`
	NextMinimum      int64          `json:"nextMinimum"`
}

func (s *RedisStore) PlaceBid(command BidCommand) (BidResult, error) {
	if command.UserID == "" || command.RequestID == "" {
		return BidResult{}, fmt.Errorf("%w: user id and request id are required", ErrBidRejected)
	}
	if command.Now.IsZero() {
		command.Now = time.Now().UTC()
	}
	ctx := context.Background()
	bidID := auction.NewID("bid")
	raw, err := s.client.Eval(ctx, placeBidScript, []string{
		AuctionSnapshotKey(command.AuctionID),
		AuctionRankKey(command.AuctionID),
		AuctionAmountKey(command.AuctionID),
		AuctionRankSeqKey(command.AuctionID),
		AuctionSeqKey(command.AuctionID),
		AuctionRequestKey(command.AuctionID, command.RequestID),
	}, command.AuctionID, command.UserID, command.RequestID, command.Amount, command.Now.UTC().UnixMilli(), bidID, int(requestTTL.Seconds())).Text()
	if err != nil {
		return BidResult{}, err
	}
	parsed := bidScriptResult{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return BidResult{}, err
	}
	if !parsed.OK {
		return BidResult{NextMinimum: parsed.NextMinimum}, scriptBidError(parsed)
	}
	snapshot, err := s.Snapshot(command.AuctionID, command.UserID)
	if err != nil {
		return BidResult{}, err
	}
	return BidResult{
		BidID:       parsed.BidID,
		Snapshot:    snapshot,
		NextMinimum: parsed.NextMinimum,
		Extended:    parsed.Extended,
		Idempotent:  parsed.Idempotent,
	}, nil
}

func scriptBidError(result bidScriptResult) error {
	switch result.Error {
	case "not_found":
		return ErrAuctionNotFound
	case "status":
		return fmt.Errorf("%w: auction status is %s", ErrBidRejected, result.Status)
	case "expired":
		return fmt.Errorf("%w: auction already ended", ErrBidRejected)
	case "low_bid":
		return fmt.Errorf("%w: amount must be at least %d", ErrBidRejected, result.NextMinimum)
	case "increment":
		return fmt.Errorf("%w: amount must follow increment", ErrBidRejected)
	default:
		return fmt.Errorf("%w: redis bid rejected", ErrBidRejected)
	}
}
```

Then fix idempotent parsing by changing the Lua existing branch to return a modified idempotent result:

```lua
if existing then
  local decoded = cjson.decode(existing)
  decoded["idempotent"] = true
  return cjson.encode(decoded)
end
```

- [ ] **Step 6: 运行 Redis 出价测试**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run "TestRedisStore(PlaceBid|Concurrent|Ceiling)"
```

Expected: PASS.

- [ ] **Step 7: 运行 Redis 包全部测试**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis
```

Expected: PASS.

- [ ] **Step 8: 提交**

Run:

```powershell
git add internal/redis/scripts.go internal/redis/redis_store.go internal/redis/redis_store_test.go internal/redis/store_test.go
git commit -m "feat: add redis atomic bidding"
```

Expected: commit succeeds.

---

### Task 6: Redis 取消和到期结算

**Files:**
- Modify: `internal/redis/scripts.go`
- Modify: `internal/redis/redis_store.go`
- Modify: `internal/redis/redis_store_test.go`

- [ ] **Step 1: 添加取消和结算测试**

Append to `internal/redis/redis_store_test.go`:

```go
func TestRedisStoreCancelAllowsDraftAndRunning(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	snapshot := startedSnapshot(now)
	snapshot.Status = auction.StatusDraft
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}

	cancelled, err := store.Cancel("auction-redis-1", now)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != auction.StatusCancelled {
		t.Fatalf("Status = %s", cancelled.Status)
	}
}

func TestRedisStoreFinishExpiredSellsWhenHighestBidderExists(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	snapshot := startedSnapshot(now)
	snapshot.EndsAt = now.Add(-time.Second)
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}
	_, err := store.PlaceBid(BidCommand{AuctionID: "auction-redis-1", UserID: "user-a", RequestID: "req-a", Amount: 100, Now: now.Add(-2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}

	finished, err := store.FinishExpired("auction-redis-1", now)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != auction.StatusSold {
		t.Fatalf("Status = %s", finished.Status)
	}
}

func TestRedisStoreFinishExpiredEndsWhenNoBidderExists(t *testing.T) {
	store, _ := newRedisStoreForTest(t)
	now := time.Unix(100, 0).UTC()
	snapshot := startedSnapshot(now)
	snapshot.EndsAt = now.Add(-time.Second)
	if err := store.InitAuction(snapshot); err != nil {
		t.Fatal(err)
	}

	finished, err := store.FinishExpired("auction-redis-1", now)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != auction.StatusEnded {
		t.Fatalf("Status = %s", finished.Status)
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run "TestRedisStore(Cancel|Finish)"
```

Expected: FAIL，提示 `RedisStore.Cancel` 或 `RedisStore.FinishExpired` 未实现。

- [ ] **Step 3: 添加 Lua 脚本**

Append to `internal/redis/scripts.go`:

```go
const cancelScript = `
local snapshotKey = KEYS[1]
local nowMs = tonumber(ARGV[1])
if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end
local status = redis.call("HGET", snapshotKey, "status")
if status ~= "DRAFT" and status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status})
end
redis.call("HSET", snapshotKey, "status", "CANCELLED", "serverTimeUnixMs", nowMs)
return cjson.encode({ok=true})
`

const finishExpiredScript = `
local snapshotKey = KEYS[1]
local nowMs = tonumber(ARGV[1])
if redis.call("EXISTS", snapshotKey) == 0 then
  return cjson.encode({ok=false, error="not_found"})
end
local status = redis.call("HGET", snapshotKey, "status")
local endsAtMs = tonumber(redis.call("HGET", snapshotKey, "endsAtUnixMs"))
local highestBidder = redis.call("HGET", snapshotKey, "highestBidder")
if status ~= "RUNNING" then
  return cjson.encode({ok=false, error="status", status=status})
end
if nowMs < endsAtMs then
  return cjson.encode({ok=false, error="not_expired"})
end
local newStatus = "ENDED"
if highestBidder and highestBidder ~= "" then
  newStatus = "SOLD"
end
redis.call("HSET", snapshotKey, "status", newStatus, "serverTimeUnixMs", nowMs)
return cjson.encode({ok=true, status=newStatus})
`
```

- [ ] **Step 4: 实现 Cancel 和 FinishExpired**

Append to `internal/redis/redis_store.go`:

```go
type stateScriptResult struct {
	OK     bool           `json:"ok"`
	Error  string         `json:"error"`
	Status auction.Status `json:"status"`
}

func (s *RedisStore) Cancel(auctionID string, now time.Time) (auction.Snapshot, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx := context.Background()
	raw, err := s.client.Eval(ctx, cancelScript, []string{AuctionSnapshotKey(auctionID)}, now.UTC().UnixMilli()).Text()
	if err != nil {
		return auction.Snapshot{}, err
	}
	parsed := stateScriptResult{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return auction.Snapshot{}, err
	}
	if !parsed.OK {
		return auction.Snapshot{}, scriptStateError(parsed, "cancel")
	}
	return s.Snapshot(auctionID, "")
}

func (s *RedisStore) FinishExpired(auctionID string, now time.Time) (auction.Snapshot, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx := context.Background()
	raw, err := s.client.Eval(ctx, finishExpiredScript, []string{AuctionSnapshotKey(auctionID)}, now.UTC().UnixMilli()).Text()
	if err != nil {
		return auction.Snapshot{}, err
	}
	parsed := stateScriptResult{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return auction.Snapshot{}, err
	}
	if !parsed.OK {
		return auction.Snapshot{}, scriptStateError(parsed, "finish")
	}
	return s.Snapshot(auctionID, "")
}

func scriptStateError(result stateScriptResult, action string) error {
	switch result.Error {
	case "not_found":
		return ErrAuctionNotFound
	case "status":
		return fmt.Errorf("%w: cannot %s status %s", ErrBidRejected, action, result.Status)
	case "not_expired":
		return fmt.Errorf("%w: auction has not ended", ErrBidRejected)
	default:
		return fmt.Errorf("%w: redis state transition rejected", ErrBidRejected)
	}
}
```

- [ ] **Step 5: 运行状态流转测试**

Run:

```powershell
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis -run "TestRedisStore(Cancel|Finish)"
```

Expected: PASS.

- [ ] **Step 6: 提交**

Run:

```powershell
git add internal/redis/scripts.go internal/redis/redis_store.go internal/redis/redis_store_test.go
git commit -m "feat: add redis state transitions"
```

Expected: commit succeeds.

---

### Task 7: Redis Pub/Sub 事件边界

**Files:**
- Create: `internal/ws/redis_bus.go`
- Modify: `internal/service/auction.go`
- Modify: `cmd/api/main.go`

- [ ] **Step 1: 添加 RedisBus 单元测试**

Create `internal/ws/redis_bus_test.go`:

```go
package ws

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestRedisBusIgnoresOwnEvents(t *testing.T) {
	event := Event{Type: EventBidAccepted, AuctionID: "auc-1"}
	if shouldForwardRedisEvent("api-1", redisEventEnvelope{SourceID: "api-1", Event: event}) {
		t.Fatal("own event should not be forwarded")
	}
}

func TestRedisBusForwardsOtherInstanceEvents(t *testing.T) {
	event := Event{Type: EventBidAccepted, AuctionID: "auc-1"}
	if !shouldForwardRedisEvent("api-1", redisEventEnvelope{SourceID: "api-2", Event: event}) {
		t.Fatal("other instance event should be forwarded")
	}
}

func TestRedisBusPublishAndSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("requires local redis")
	}
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	defer client.Close()

	bus := NewRedisBus(client, "api-1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events, stop, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	other := NewRedisBus(client, "api-2")
	if err := other.Publish(ctx, Event{Type: EventBidAccepted, AuctionID: "auc-1"}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-events:
		if event.AuctionID != "auc-1" || event.Type != EventBidAccepted {
			t.Fatalf("event = %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for redis event")
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run:

```powershell
go test -count=1 ./internal/ws -run TestRedisBus
```

Expected: FAIL，提示 `NewRedisBus` 或 `shouldForwardRedisEvent` 未定义。

- [ ] **Step 3: 实现 RedisBus**

Create `internal/ws/redis_bus.go`:

```go
package ws

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const redisEventChannel = "auction:events"

type RedisBus struct {
	client   *goredis.Client
	sourceID string
}

type redisEventEnvelope struct {
	SourceID string `json:"sourceId"`
	Event    Event  `json:"event"`
}

func NewRedisBus(client *goredis.Client, sourceID string) *RedisBus {
	return &RedisBus{client: client, sourceID: sourceID}
}

func (b *RedisBus) Publish(ctx context.Context, event Event) error {
	body, err := json.Marshal(redisEventEnvelope{SourceID: b.sourceID, Event: event})
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, redisEventChannel, body).Err()
}

func (b *RedisBus) Subscribe(ctx context.Context) (<-chan Event, func(), error) {
	pubsub := b.client.Subscribe(ctx, redisEventChannel)
	if _, err := pubsub.ReceiveTimeout(ctx, 3*time.Second); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(out)
		ch := pubsub.Channel()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case msg := <-ch:
				if msg == nil {
					continue
				}
				envelope := redisEventEnvelope{}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					continue
				}
				if shouldForwardRedisEvent(b.sourceID, envelope) {
					out <- envelope.Event
				}
			}
		}
	}()
	stop := func() {
		close(done)
		_ = pubsub.Close()
	}
	return out, stop, nil
}

func shouldForwardRedisEvent(sourceID string, envelope redisEventEnvelope) bool {
	return envelope.SourceID != "" && envelope.SourceID != sourceID && envelope.Event.AuctionID != ""
}
```

- [ ] **Step 4: 给 AuctionService 增加事件发布边界**

In `internal/service/auction.go`, add imports:

```go
	"context"
	"log"
```

Add interface and field near `AuctionService`:

```go
type EventPublisher interface {
	Publish(ctx context.Context, event ws.Event) error
}

type AuctionService struct {
	repo           repository.AuctionRepository
	store          redis.Store
	hub            *ws.Hub
	eventPublisher EventPublisher
	settlement     *SettlementService
	timerMu        sync.Mutex
	timers         map[string]*time.Timer
}
```

Add methods:

```go
func (s *AuctionService) SetEventPublisher(publisher EventPublisher) {
	s.eventPublisher = publisher
}

func (s *AuctionService) BroadcastExternal(event ws.Event) {
	s.hub.Broadcast(event)
}

func (s *AuctionService) broadcast(event ws.Event) {
	s.hub.Broadcast(event)
	if s.eventPublisher == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.eventPublisher.Publish(ctx, event); err != nil {
		log.Printf("publish redis auction event: %v", err)
	}
}
```

Replace each `s.hub.Broadcast(` call in `internal/service/auction.go` with `s.broadcast(`.

- [ ] **Step 5: 启动 Redis 事件订阅桥**

In `cmd/api/main.go`, after `auctionService := service.NewAuctionService(repo, store, hub)`, add:

```go
if redisClient != nil {
	bus := ws.NewRedisBus(redisClient, "api-local")
	auctionService.SetEventPublisher(bus)
	ctx := context.Background()
	events, stop, err := bus.Subscribe(ctx)
	if err != nil {
		log.Printf("redis event subscribe disabled: %v", err)
	} else {
		defer stop()
		go func() {
			for event := range events {
				auctionService.BroadcastExternal(event)
			}
		}()
	}
}
```

- [ ] **Step 6: 运行 ws 和 service 测试**

Run:

```powershell
go test -count=1 ./internal/ws ./internal/service
```

Expected: PASS.

- [ ] **Step 7: 提交**

Run:

```powershell
git add internal/ws/redis_bus.go internal/ws/redis_bus_test.go internal/service/auction.go cmd/api/main.go
git commit -m "feat: add redis auction event bus"
```

Expected: commit succeeds.

---

### Task 8: 后端启动接入 Redis

**Files:**
- Modify: `cmd/api/main.go`

- [ ] **Step 1: 修改 main.go 使用真实 RedisStore**

Replace `cmd/api/main.go` with:

```go
package main

import (
	"context"
	"log"
	nethttp "net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/config"
	apphttp "realtime-auction-core/internal/http"
	rtredis "realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

func main() {
	cfg := config.Load()
	repo, err := repository.NewFileRepository("data")
	if err != nil {
		log.Fatal(err)
	}

	redisClient := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var store rtredis.Store
	if err := redisClient.Ping(ctx).Err(); err != nil {
		if cfg.RedisRequired {
			log.Fatalf("redis unavailable at %s: %v; run docker compose up -d redis mysql", cfg.RedisAddr, err)
		}
		log.Printf("redis unavailable at %s, using memory store because REDIS_REQUIRED=false: %v", cfg.RedisAddr, err)
		store = rtredis.NewMemoryStore()
	} else {
		store = rtredis.NewRedisStore(redisClient)
	}
	defer redisClient.Close()

	hub := ws.NewHub()
	auctionService := service.NewAuctionService(repo, store, hub)
	if typedStore, ok := store.(*rtredis.RedisStore); ok && typedStore != nil {
		bus := ws.NewRedisBus(redisClient, "api-local")
		auctionService.SetEventPublisher(bus)
		events, stop, err := bus.Subscribe(context.Background())
		if err != nil {
			log.Printf("redis event subscribe disabled: %v", err)
		} else {
			defer stop()
			go func() {
				for event := range events {
					auctionService.BroadcastExternal(event)
				}
			}()
		}
	}
	authService := service.NewAuthService(repo, cfg.JWTSecret, cfg.JWTTTL)
	if err := authService.SeedDemoUsers(); err != nil {
		log.Fatal(err)
	}
	server := apphttp.NewServer(auctionService, authService)

	log.Printf("auction api listening on %s", cfg.HTTPAddr)
	if err := nethttp.ListenAndServe(cfg.HTTPAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 2: 运行后端包测试**

Run:

```powershell
go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 3: 手动启动后端验证 Redis 连接**

Run:

```powershell
docker compose up -d redis mysql
$env:REDIS_ADDR="127.0.0.1:6379"
$env:HTTP_ADDR="127.0.0.1:18080"
go run ./cmd/api
```

Expected log:

```text
auction api listening on 127.0.0.1:18080
```

Stop the process with `Ctrl+C`.

- [ ] **Step 4: 手动验证 Redis 不可用时失败**

Run:

```powershell
docker compose stop redis
$env:REDIS_ADDR="127.0.0.1:6379"
$env:REDIS_REQUIRED="true"
go run ./cmd/api
```

Expected log contains:

```text
redis unavailable at 127.0.0.1:6379
run docker compose up -d redis mysql
```

Restart Redis:

```powershell
docker compose up -d redis
```

- [ ] **Step 5: 提交**

Run:

```powershell
git add cmd/api/main.go
git commit -m "feat: wire api to redis store"
```

Expected: commit succeeds.

---

### Task 9: E2E 与启动配置适配

**Files:**
- Modify: `web/playwright.config.ts`

- [ ] **Step 1: 更新 Playwright 后端启动命令**

In `web/playwright.config.ts`, replace backend webServer command:

```ts
command: "set HTTP_ADDR=127.0.0.1:18080&& go run ../cmd/api",
```

with:

```ts
command: "set HTTP_ADDR=127.0.0.1:18080&& set REDIS_ADDR=127.0.0.1:6379&& go run ../cmd/api",
```

- [ ] **Step 2: 运行前端构建**

Run:

```powershell
cd web
npm run build
```

Expected:

```text
✓ built
```

- [ ] **Step 3: 运行 E2E**

Run:

```powershell
docker compose up -d redis mysql
cd web
npm run test:e2e
```

Expected:

```text
passed
```

- [ ] **Step 4: 提交**

Run:

```powershell
git add web/playwright.config.ts
git commit -m "test: run e2e against redis backend"
```

Expected: commit succeeds.

---

### Task 10: 最终验证与清理

**Files:**
- Inspect: all modified files

- [ ] **Step 1: 运行 Go 全量测试**

Run:

```powershell
go test -count=1 ./...
```

Expected: PASS.

- [ ] **Step 2: 运行 Redis 集成测试**

Run:

```powershell
docker compose up -d redis mysql
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis
```

Expected: PASS.

- [ ] **Step 3: 运行前端构建**

Run:

```powershell
cd web
npm run build
```

Expected: PASS.

- [ ] **Step 4: 运行 E2E**

Run:

```powershell
cd web
npm run test:e2e
```

Expected: PASS.

- [ ] **Step 5: 检查是否仍有生产路径使用 MemoryStore**

Run:

```powershell
rg "NewMemoryStore" cmd internal
```

Expected:

```text
internal/redis/store.go
internal/redis/store_test.go
cmd/api/main.go
```

`cmd/api/main.go` 中只允许出现在 `REDIS_REQUIRED=false` 的显式分支。

- [ ] **Step 6: 检查工作区状态**

Run:

```powershell
git status --short
```

Expected: no output.

---

## 自检

**Spec coverage:**

- Docker Redis 与 MySQL 预留：Task 1。
- 真实 `RedisStore` 实现现有 `redis.Store` 接口：Task 4、5、6。
- 出价校验、价格更新、排名、幂等由 Redis Lua 原子完成：Task 5。
- 取消和到期结算状态流转：Task 6。
- Redis Pub/Sub 事件边界：Task 7。
- 后端启动强依赖 Redis，默认不回退到内存：Task 8。
- 文件 repository 继续承载业务事实：所有任务都不改 `internal/repository`。
- 前端接口和 WebSocket 体验不重构：Task 9 只改 E2E 启动环境。

**Placeholder scan:**

- 未使用占位式实现说明。
- 每个代码变更步骤都给出目标文件、命令和期望结果。

**Type consistency:**

- 运行时 Store 类型统一为 `rtredis.Store`。
- Redis client 外部包统一别名 `goredis`，内部 Redis 包统一别名 `rtredis`。
- Key 函数名在测试、脚本调用和 Store 实现中一致。
