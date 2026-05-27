# JWT User Ranking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把当前演示登录改成预置账号 + JWT 鉴权，并在管理端和用户端实时展示 Top 5 排行榜。

**Architecture:** 后端继续保持 domain/service/repository/http 分层。用户账号写入本地 JSON 文件，AuthService 负责 seed、密码校验和 JWT 签发/验证；竞拍快照增加 leaderboard，由实时 store 计算排序，service 补 displayName 后广播给前端。前端登录页改为 username/password，管理端和用户端都用 WebSocket snapshot 渲染实时价格、状态、倒计时、日志和 Top 5。

**Tech Stack:** Go 标准库、React、TypeScript、Vite、Playwright、本地 JSON 文件、WebSocket、HMAC-SHA256 JWT。

---

## 文件边界

- `internal/domain/auction/model.go`：扩展 `User`、增加 `UserStatus`、增加 `LeaderboardEntry` 和 `Snapshot.Leaderboard`。
- `internal/repository/repository.go`：增加用户查询和 upsert 接口，逐步废弃 session 接口。
- `internal/repository/memory.go`：实现 username 查询、用户 upsert、用户列表。
- `internal/repository/file.go`：实现 username 查询、用户 upsert、用户列表，继续兼容受限 Windows 文件写入。
- `internal/repository/file_test.go`：覆盖用户 seed 所需的持久化和 username 查询。
- `internal/service/auth.go`：重写为预置账号 seed、密码 hash、JWT 签发和 JWT 鉴权。
- `internal/service/auth_test.go`：覆盖账号密码、JWT、禁用用户、角色权限。
- `internal/config/config.go`：增加 `JWTSecret` 和 `JWTTTL` 默认配置。
- `cmd/api/main.go`：启动时调用 `SeedDemoUsers()`。
- `internal/redis/store.go`：记录排名金额和出价顺序，输出 Top 5 leaderboard。
- `internal/redis/store_test.go`：覆盖 Top 5、同价先到先得、当前用户 rank。
- `internal/service/auction.go`：将 leaderboard 中 userId 补成 displayName，广播含 leaderboard 的 snapshot。
- `internal/service/auction_test.go`：覆盖 service 层补 displayName。
- `internal/http/server.go`：登录请求改为 username/password，所有鉴权改为 JWT。
- `internal/http/server_test.go`：覆盖真实账号登录、JWT 鉴权、刷新后稳定用户、出价忽略请求体 userId。
- `web/src/api.ts`：更新登录请求/响应类型，增加 leaderboard 类型。
- `web/src/session.ts`：继续保存 JWT 登录结果，不再保存临时昵称角色。
- `web/src/LoginPage.tsx`：改为用户名和密码登录，不再选择角色。
- `web/src/AdminApp.tsx`：展示 Top 5 排行榜，使用 displayName 显示最高出价人和日志。
- `web/src/MobileApp.tsx`：展示 Top 5 排行榜和我的排名，刷新后用 JWT 恢复。
- `web/src/styles.css`：增加排行榜样式。
- `web/tests/realtime-ranking.spec.ts`：改成 admin/userA/userB 多上下文登录，验证排行实时刷新和刷新恢复。

## 任务 1：用户领域模型和 Repository 边界

**Files:**
- Modify: `internal/domain/auction/model.go`
- Modify: `internal/repository/repository.go`
- Modify: `internal/repository/memory.go`
- Modify: `internal/repository/file.go`
- Modify: `internal/repository/file_test.go`

- [ ] **Step 1: 写失败测试，覆盖用户按 username 持久化查询**

在 `internal/repository/file_test.go` 增加：

```go
func TestFileRepositoryPersistsUsersByUsername(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Round(0)
	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}
	user := auction.User{
		ID:           "usr-user-a",
		Username:     "userA",
		DisplayName:  "用户A",
		PasswordHash: "hash-a",
		PasswordSalt: "salt-a",
		Role:         auction.RoleBidder,
		Status:       auction.UserActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.UpsertUser(user); err != nil {
		t.Fatalf("UpsertUser() error = %v", err)
	}
	reloaded, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository(reloaded) error = %v", err)
	}
	got, err := reloaded.GetUserByUsername("userA")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}
	if got != user {
		t.Fatalf("GetUserByUsername() = %+v, want %+v", got, user)
	}
	items, err := reloaded.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(items) != 1 || items[0] != user {
		t.Fatalf("ListUsers() = %+v, want [%+v]", items, user)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```powershell
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/repository
```

Expected: FAIL，提示 `UpsertUser`、`GetUserByUsername`、`ListUsers` 或 `UserActive` 未定义。

- [ ] **Step 3: 扩展领域模型**

在 `internal/domain/auction/model.go` 中把 `User` 调整为：

```go
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"displayName"`
	PasswordHash string     `json:"-"`
	PasswordSalt string     `json:"-"`
	Role         Role       `json:"role"`
	Status       UserStatus `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}
```

如果现有代码还读取 `user.Name`，后续任务统一改成 `DisplayName`。

- [ ] **Step 4: 扩展 repository 接口**

在 `internal/repository/repository.go` 中加入：

```go
UpsertUser(auction.User) error
GetUser(id string) (auction.User, error)
GetUserByUsername(username string) (auction.User, error)
ListUsers() ([]auction.User, error)
```

保留旧的 `SaveUser`、`SaveSession`、`GetUserByToken`，本任务只扩展边界，避免一次性打断旧 auth 测试。

- [ ] **Step 5: 实现 MemoryRepository 用户方法**

在 `internal/repository/memory.go` 中补：

```go
func (r *MemoryRepository) UpsertUser(user auction.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[user.ID] = user
	return nil
}

func (r *MemoryRepository) GetUserByUsername(username string) (auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.Username == username {
			return user, nil
		}
	}
	return auction.User{}, ErrNotFound
}

func (r *MemoryRepository) ListUsers() ([]auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]auction.User, 0, len(r.users))
	for _, user := range r.users {
		items = append(items, user)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Username < items[j].Username
	})
	return items, nil
}
```

同时给 `memory.go` 增加 `sort` import。

- [ ] **Step 6: 实现 FileRepository 用户方法**

在 `internal/repository/file.go` 中补：

```go
func (r *FileRepository) UpsertUser(user auction.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	users := cloneMap(r.users)
	users[user.ID] = user
	if err := r.save(usersFile, users); err != nil {
		return err
	}
	r.users = users
	return nil
}

func (r *FileRepository) GetUserByUsername(username string) (auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.Username == username {
			return user, nil
		}
	}
	return auction.User{}, ErrNotFound
}

func (r *FileRepository) ListUsers() ([]auction.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]auction.User, 0, len(r.users))
	for _, user := range r.users {
		items = append(items, user)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Username < items[j].Username
	})
	return items, nil
}
```

- [ ] **Step 7: 兼容旧 SaveUser**

把 `MemoryRepository.SaveUser` 和 `FileRepository.SaveUser` 改为调用 `UpsertUser`：

```go
func (r *MemoryRepository) SaveUser(user auction.User) error {
	return r.UpsertUser(user)
}
```

`FileRepository.SaveUser` 同理。

- [ ] **Step 8: 格式化并验证**

Run:

```powershell
gofmt -w internal\domain\auction\model.go internal\repository\repository.go internal\repository\memory.go internal\repository\file.go internal\repository\file_test.go
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/repository ./internal/domain/auction
```

Expected: PASS。

- [ ] **Step 9: 提交**

```powershell
git add internal/domain/auction/model.go internal/repository/repository.go internal/repository/memory.go internal/repository/file.go internal/repository/file_test.go
git commit -m "feat: add stable user repository fields"
```

## 任务 2：JWT AuthService 与预置账号 seed

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/service/auth.go`
- Modify: `internal/service/auth_test.go`
- Modify: `cmd/api/main.go`

- [ ] **Step 1: 写 AuthService 失败测试**

替换或扩展 `internal/service/auth_test.go`，保留 storage 错误测试时同步新接口。增加以下测试：

```go
func TestAuthSeedsDemoUsersAndLogsInWithJWT(t *testing.T) {
	repo := repository.NewMemoryRepository()
	auth := NewAuthService(repo, "demo-secret", 24*time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatalf("SeedDemoUsers() error = %v", err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.User.ID != "usr-user-a" || session.User.DisplayName != "用户A" || session.User.Role != auction.RoleBidder {
		t.Fatalf("Login() user = %+v", session.User)
	}
	user, err := auth.Require(session.Token, auction.RoleBidder)
	if err != nil {
		t.Fatalf("Require() error = %v", err)
	}
	if user.ID != "usr-user-a" {
		t.Fatalf("Require() user id = %s, want usr-user-a", user.ID)
	}
}

func TestAuthLoginRejectsWrongPassword(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Login("userA", "bad-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want %v", err, ErrInvalidCredentials)
	}
}

func TestAuthRequireRejectsWrongRole(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", time.Hour)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Require(session.Token, auction.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Require(admin) error = %v, want %v", err, ErrForbidden)
	}
}

func TestAuthRequireRejectsExpiredJWT(t *testing.T) {
	auth := NewAuthService(repository.NewMemoryRepository(), "demo-secret", -time.Second)
	if err := auth.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	session, err := auth.Login("userA", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Require(session.Token, auction.RoleBidder); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Require(expired) error = %v, want %v", err, ErrTokenExpired)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```powershell
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/service -run Auth
```

Expected: FAIL，提示 `NewAuthService` 参数不匹配、`SeedDemoUsers`、`ErrInvalidCredentials` 或 `ErrTokenExpired` 未定义。

- [ ] **Step 3: 更新 config**

在 `internal/config/config.go` 的 `Config` 增加：

```go
JWTSecret string
JWTTTL    time.Duration
```

增加 `time` import，并在 `Load()` 返回：

```go
JWTSecret: env("JWT_SECRET", "local-demo-jwt-secret"),
JWTTTL:    24 * time.Hour,
```

- [ ] **Step 4: 实现 AuthService 构造和错误**

在 `internal/service/auth.go` 中定义：

```go
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrUserDisabled = errors.New("user disabled")
var ErrTokenExpired = errors.New("token expired")
var ErrInvalidToken = errors.New("invalid token")

type AuthService struct {
	repo      repository.AuctionRepository
	jwtSecret []byte
	jwtTTL    time.Duration
}

func NewAuthService(repo repository.AuctionRepository, jwtSecret string, jwtTTL time.Duration) *AuthService {
	if jwtSecret == "" {
		jwtSecret = "local-demo-jwt-secret"
	}
	if jwtTTL == 0 {
		jwtTTL = 24 * time.Hour
	}
	return &AuthService{repo: repo, jwtSecret: []byte(jwtSecret), jwtTTL: jwtTTL}
}
```

- [ ] **Step 5: 实现 demo 用户 seed**

在 `auth.go` 中增加：

```go
type demoUserSeed struct {
	ID          string
	Username    string
	Password    string
	DisplayName string
	Role        auction.Role
}

var demoUsers = []demoUserSeed{
	{ID: "usr-admin", Username: "admin", Password: "admin123", DisplayName: "管理员", Role: auction.RoleAdmin},
	{ID: "usr-user-a", Username: "userA", Password: "123456", DisplayName: "用户A", Role: auction.RoleBidder},
	{ID: "usr-user-b", Username: "userB", Password: "123456", DisplayName: "用户B", Role: auction.RoleBidder},
	{ID: "usr-user-c", Username: "userC", Password: "123456", DisplayName: "用户C", Role: auction.RoleBidder},
}

func (s *AuthService) SeedDemoUsers() error {
	now := time.Now().UTC()
	for _, seed := range demoUsers {
		salt := "demo:" + seed.Username
		user := auction.User{
			ID:           seed.ID,
			Username:     seed.Username,
			DisplayName:  seed.DisplayName,
			PasswordSalt: salt,
			PasswordHash: hashPassword(salt, seed.Password),
			Role:         seed.Role,
			Status:       auction.UserActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if existing, err := s.repo.GetUser(seed.ID); err == nil && !existing.CreatedAt.IsZero() {
			user.CreatedAt = existing.CreatedAt
		}
		if err := s.repo.UpsertUser(user); err != nil {
			return ErrAuthStorage
		}
	}
	return nil
}
```

- [ ] **Step 6: 实现密码 hash**

在 `auth.go` import `crypto/sha256`，增加：

```go
func hashPassword(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + password))
	return hex.EncodeToString(sum[:])
}
```

复用已有 `encoding/hex` import。

- [ ] **Step 7: 实现 JWT 签发和验证**

在 `auth.go` import `crypto/hmac`、`encoding/base64`、`encoding/json`。

增加：

```go
type jwtClaims struct {
	Subject  string       `json:"sub"`
	Username string       `json:"username"`
	Role     auction.Role `json:"role"`
	IssuedAt int64        `json:"iat"`
	Expires  int64        `json:"exp"`
}

func (s *AuthService) signJWT(user auction.User, now time.Time) (string, error) {
	claims := jwtClaims{
		Subject:  user.ID,
		Username: user.Username,
		Role:     user.Role,
		IssuedAt: now.Unix(),
		Expires:  now.Add(s.jwtTTL).Unix(),
	}
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := s.jwtSignature(unsigned)
	return unsigned + "." + sig, nil
}

func (s *AuthService) jwtSignature(unsigned string) string {
	mac := hmac.New(sha256.New, s.jwtSecret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
```

增加 `parseJWT(token string, now time.Time) (jwtClaims, error)`，要求：

```go
parts := strings.Split(token, ".")
if len(parts) != 3 { return jwtClaims{}, ErrInvalidToken }
unsigned := parts[0] + "." + parts[1]
if !hmac.Equal([]byte(parts[2]), []byte(s.jwtSignature(unsigned))) { return jwtClaims{}, ErrInvalidToken }
payload, err := base64.RawURLEncoding.DecodeString(parts[1])
if err != nil { return jwtClaims{}, ErrInvalidToken }
var claims jwtClaims
if err := json.Unmarshal(payload, &claims); err != nil { return jwtClaims{}, ErrInvalidToken }
if claims.Expires <= now.Unix() { return jwtClaims{}, ErrTokenExpired }
if claims.Subject == "" { return jwtClaims{}, ErrInvalidToken }
return claims, nil
```


- [ ] **Step 8: 实现 Login 和 Require**

把 `Login` 改为：

```go
func (s *AuthService) Login(username, password string) (LoginSession, error) {
	username = strings.TrimSpace(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return LoginSession{}, ErrInvalidCredentials
	}
	user, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return LoginSession{}, ErrInvalidCredentials
	}
	if user.Status != auction.UserActive {
		return LoginSession{}, ErrUserDisabled
	}
	if user.PasswordHash != hashPassword(user.PasswordSalt, password) {
		return LoginSession{}, ErrInvalidCredentials
	}
	token, err := s.signJWT(user, time.Now().UTC())
	if err != nil {
		return LoginSession{}, err
	}
	return LoginSession{Token: token, User: user}, nil
}
```

把 `Require` 改为解析 JWT 后查用户：

```go
func (s *AuthService) Require(token string, role auction.Role) (auction.User, error) {
	claims, err := s.parseJWT(strings.TrimSpace(token), time.Now().UTC())
	if err != nil {
		return auction.User{}, err
	}
	user, err := s.repo.GetUser(claims.Subject)
	if err != nil {
		return auction.User{}, ErrUnauthorized
	}
	if user.Status != auction.UserActive {
		return auction.User{}, ErrUserDisabled
	}
	if user.Role != role {
		return auction.User{}, ErrForbidden
	}
	return user, nil
}
```

- [ ] **Step 9: main.go seed demo users**

在 `cmd/api/main.go` 中创建 auth service 后调用：

```go
authService := service.NewAuthService(repo, cfg.JWTSecret, cfg.JWTTTL)
if err := authService.SeedDemoUsers(); err != nil {
	log.Fatal(err)
}
```

- [ ] **Step 10: 格式化并验证**

Run:

```powershell
gofmt -w internal\config\config.go internal\service\auth.go internal\service\auth_test.go cmd\api\main.go
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/service ./internal/config ./cmd/api
```

Expected: PASS。

- [ ] **Step 11: 提交**

```powershell
git add internal/config/config.go internal/service/auth.go internal/service/auth_test.go cmd/api/main.go
git commit -m "feat: add jwt demo auth"
```

## 任务 3：HTTP 登录和 JWT 角色鉴权

**Files:**
- Modify: `internal/http/server.go`
- Modify: `internal/http/server_test.go`

- [ ] **Step 1: 写 HTTP 失败测试**

在 `internal/http/server_test.go` 更新登录 helper，并增加：

```go
func TestLoginWithPasswordAndRoleProtectedAdminCreate(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	bidder := loginWithPassword(t, ts, "userA", "123456")
	blocked := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), bidder.Token)
	if blocked.Code != nethttp.StatusForbidden {
		t.Fatalf("bidder admin create status = %d body=%s", blocked.Code, blocked.Body.String())
	}

	admin := loginWithPassword(t, ts, "admin", "admin123")
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), admin.Token)
	if created.Code != nethttp.StatusCreated {
		t.Fatalf("admin create status = %d body=%s", created.Code, created.Body.String())
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	res := postJSON(t, ts, "/api/login", map[string]any{"username": "userA", "password": "bad"}, "")
	if res.Code != nethttp.StatusUnauthorized {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	var body map[string]any
	decodeBody(t, res.Body.Bytes(), &body)
	if body["code"] != "INVALID_CREDENTIALS" {
		t.Fatalf("code = %#v, want INVALID_CREDENTIALS", body["code"])
	}
}
```

把旧 `loginAs` helper 改为：

```go
func loginWithPassword(t *testing.T, ts *httptest.Server, username, password string) service.LoginSession {
	t.Helper()
	res := postJSON(t, ts, "/api/login", map[string]any{"username": username, "password": password}, "")
	if res.Code != nethttp.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	var session service.LoginSession
	decodeBody(t, res.Body.Bytes(), &session)
	if session.User.Username != username {
		t.Fatalf("login username = %s, want %s", session.User.Username, username)
	}
	return session
}
```

- [ ] **Step 2: 更新 test server seed**

在 `newHTTPTestServer` 中：

```go
authService := service.NewAuthService(repo, "demo-secret", 24*time.Hour)
if err := authService.SeedDemoUsers(); err != nil {
	t.Fatal(err)
}
```

- [ ] **Step 3: 运行测试确认失败**

Run:

```powershell
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/http
```

Expected: FAIL，当前 `/api/login` 仍读取 name/role 或 auth service 构造未更新。

- [ ] **Step 4: 修改登录请求结构**

在 `internal/http/server.go` 中改：

```go
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
```

`login` 调用：

```go
session, err := s.auth.Login(req.Username, req.Password)
```

- [ ] **Step 5: 修改鉴权错误映射**

在 `writeAuthError` 中增加：

```go
case errors.Is(err, service.ErrInvalidCredentials):
	writeAPIError(w, nethttp.StatusUnauthorized, "INVALID_CREDENTIALS", "账号或密码错误", nil)
case errors.Is(err, service.ErrUserDisabled):
	writeAPIError(w, nethttp.StatusForbidden, "USER_DISABLED", "用户已禁用", nil)
case errors.Is(err, service.ErrTokenExpired):
	writeAPIError(w, nethttp.StatusUnauthorized, "TOKEN_EXPIRED", "登录已过期", nil)
case errors.Is(err, service.ErrInvalidToken):
	writeAPIError(w, nethttp.StatusUnauthorized, "UNAUTHORIZED", "请先登录", nil)
```

保留 `FORBIDDEN` 和 `AUTH_STORAGE_ERROR` 分支。

- [ ] **Step 6: 确认 WebSocket 仍用 requireAnyToken**

检查 `websocketEvents`：

```go
user, ok := s.requireAnyToken(w, r.URL.Query().Get("token"))
if !ok {
	return
}
```

不要把 JWT token 当 session token 使用。

- [ ] **Step 7: 格式化并验证**

Run:

```powershell
gofmt -w internal\http\server.go internal\http\server_test.go
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/http ./internal/service
```

Expected: PASS。

- [ ] **Step 8: 提交**

```powershell
git add internal/http/server.go internal/http/server_test.go
git commit -m "feat: require password jwt login"
```

## 任务 4：实时 Top 5 排行榜快照

**Files:**
- Modify: `internal/domain/auction/model.go`
- Modify: `internal/redis/store.go`
- Modify: `internal/redis/store_test.go`
- Modify: `internal/service/auction.go`
- Modify: `internal/service/auction_test.go`

- [ ] **Step 1: 写 store 排行榜失败测试**

在 `internal/redis/store_test.go` 增加：

```go
func TestStoreSnapshotIncludesTopFiveLeaderboardAndRank(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(100, 0).UTC()
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:      "auction-1",
		Status:         auction.StatusRunning,
		CurrentPrice:   0,
		EndsAt:         now.Add(time.Minute),
		ServerTime:     now,
		NextMinimumBid: 100,
		Rules: auction.Rules{
			StartPrice:   0,
			Increment:    100,
			CeilingPrice: 2000,
		},
	}); err != nil {
		t.Fatal(err)
	}
	for i, bid := range []struct {
		user   string
		amount int64
	}{
		{"usr-user-a", 100},
		{"usr-user-b", 200},
		{"usr-user-c", 300},
		{"usr-user-d", 400},
		{"usr-user-e", 500},
		{"usr-user-f", 600},
	} {
		_, err := store.PlaceBid(BidCommand{
			AuctionID: "auction-1",
			UserID:    bid.user,
			RequestID: fmt.Sprintf("req-%d", i),
			Amount:    bid.amount,
			Now:       now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("PlaceBid(%s) error = %v", bid.user, err)
		}
	}
	snapshot, err := store.Snapshot("auction-1", "usr-user-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Leaderboard) != 5 {
		t.Fatalf("leaderboard len = %d, want 5: %+v", len(snapshot.Leaderboard), snapshot.Leaderboard)
	}
	if snapshot.Leaderboard[0].UserID != "usr-user-f" || snapshot.Leaderboard[0].Amount != 600 {
		t.Fatalf("leaderboard[0] = %+v", snapshot.Leaderboard[0])
	}
	if snapshot.Rank != 6 {
		t.Fatalf("rank = %d, want 6", snapshot.Rank)
	}
}

func TestStoreLeaderboardKeepsEarlierBidFirstOnTie(t *testing.T) {
	store := NewMemoryStore()
	now := time.Unix(200, 0).UTC()
	if err := store.InitAuction(auction.Snapshot{
		AuctionID:      "auction-1",
		Status:         auction.StatusRunning,
		EndsAt:         now.Add(time.Minute),
		ServerTime:     now,
		NextMinimumBid: 100,
		Rules: auction.Rules{Increment: 100, CeilingPrice: 1000},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PlaceBid(BidCommand{AuctionID: "auction-1", UserID: "usr-user-a", RequestID: "req-a", Amount: 100, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PlaceBid(BidCommand{AuctionID: "auction-1", UserID: "usr-user-b", RequestID: "req-b", Amount: 100, Now: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Snapshot("auction-1", "usr-user-b")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Leaderboard[0].UserID != "usr-user-a" || snapshot.Leaderboard[1].UserID != "usr-user-b" {
		t.Fatalf("leaderboard tie order = %+v", snapshot.Leaderboard)
	}
	if snapshot.Rank != 2 {
		t.Fatalf("rank = %d, want 2", snapshot.Rank)
	}
}
```

Add `fmt` import if missing.

- [ ] **Step 2: 运行测试确认失败**

Run:

```powershell
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/redis
```

Expected: FAIL，`Snapshot.Leaderboard` 未定义或 tie 排序未实现。

- [ ] **Step 3: 增加 LeaderboardEntry**

在 `internal/domain/auction/model.go` 增加：

```go
type LeaderboardEntry struct {
	Rank        int    `json:"rank"`
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Amount      int64  `json:"amount"`
}
```

在 `Snapshot` 增加：

```go
Leaderboard []LeaderboardEntry `json:"leaderboard"`
```

- [ ] **Step 4: 调整 redis store 排名结构**

在 `internal/redis/store.go` 增加内部类型：

```go
type rankingEntry struct {
	Amount int64
	Seq    int64
}
```

把 `rankings map[string]map[string]int64` 改为：

```go
rankings map[string]map[string]rankingEntry
bidSeq   int64
```

`NewMemoryStore` 初始化：

```go
rankings: map[string]map[string]rankingEntry{},
```

- [ ] **Step 5: 出价时更新排名序号**

在 `PlaceBid` 成功分支中：

```go
s.bidSeq++
existing, hasExisting := s.rankings[command.AuctionID][command.UserID]
if !hasExisting || command.Amount > existing.Amount {
	s.rankings[command.AuctionID][command.UserID] = rankingEntry{Amount: command.Amount, Seq: s.bidSeq}
}
```

只有用户刷新自己的最高价时更新 seq；低于自己最高价的情况已由出价规则挡住。

- [ ] **Step 6: 实现 leaderboard 排序**

替换 `snapshotWithRank` 为：

```go
func snapshotWithRank(snapshot auction.Snapshot, ranking map[string]rankingEntry, userID string) auction.Snapshot {
	type row struct {
		userID string
		entry  rankingEntry
	}
	rows := make([]row, 0, len(ranking))
	for id, entry := range ranking {
		rows = append(rows, row{userID: id, entry: entry})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].entry.Amount == rows[j].entry.Amount {
			return rows[i].entry.Seq < rows[j].entry.Seq
		}
		return rows[i].entry.Amount > rows[j].entry.Amount
	})
	snapshot.Rank = 0
	snapshot.Leaderboard = snapshot.Leaderboard[:0]
	for i, row := range rows {
		rank := i + 1
		if row.userID == userID {
			snapshot.Rank = rank
		}
		if i < 5 {
			snapshot.Leaderboard = append(snapshot.Leaderboard, auction.LeaderboardEntry{
				Rank:   rank,
				UserID: row.userID,
				Amount: row.entry.Amount,
			})
		}
	}
	snapshot.Participants = len(rows)
	return snapshot
}
```

- [ ] **Step 7: service 补 displayName**

在 `internal/service/auction.go` 增加 helper：

```go
func (s *AuctionService) enrichLeaderboard(snapshot auction.Snapshot) auction.Snapshot {
	for i := range snapshot.Leaderboard {
		user, err := s.repo.GetUser(snapshot.Leaderboard[i].UserID)
		if err == nil && user.DisplayName != "" {
			snapshot.Leaderboard[i].DisplayName = user.DisplayName
		} else {
			snapshot.Leaderboard[i].DisplayName = snapshot.Leaderboard[i].UserID
		}
	}
	return snapshot
}
```

在 `Snapshot`、`PlaceBid` 成功后广播前、`FinishExpired` 广播前调用：

```go
snapshot = s.enrichLeaderboard(snapshot)
```

对于 `PlaceBid` 的 `result.Snapshot`：

```go
result.Snapshot = s.enrichLeaderboard(result.Snapshot)
```

- [ ] **Step 8: 写 service displayName 测试**

在 `internal/service/auction_test.go` 增加：

```go
func TestAuctionSnapshotEnrichesLeaderboardDisplayNames(t *testing.T) {
	repo := repository.NewMemoryRepository()
	now := time.Now().UTC()
	if err := repo.UpsertUser(auction.User{ID: "usr-user-a", Username: "userA", DisplayName: "用户A", Role: auction.RoleBidder, Status: auction.UserActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	svc := NewAuctionService(repo, redis.NewMemoryStore(), ws.NewHub())
	a, err := svc.CreateAuction("merchant", auction.Product{Name: "Lot"}, auction.Rules{StartPrice: 0, Increment: 100, Duration: time.Minute, CeilingPrice: 1000})
	if err != nil {
		t.Fatal(err)
	}
	started, err := svc.StartAuction(a.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.PlaceBid(redis.BidCommand{AuctionID: a.ID, UserID: "usr-user-a", RequestID: "req-a", Amount: 100, Now: started.ServerTime.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Leaderboard) != 1 || result.Snapshot.Leaderboard[0].DisplayName != "用户A" {
		t.Fatalf("leaderboard = %+v", result.Snapshot.Leaderboard)
	}
}
```

- [ ] **Step 9: 格式化并验证**

Run:

```powershell
gofmt -w internal\domain\auction\model.go internal\redis\store.go internal\redis\store_test.go internal\service\auction.go internal\service\auction_test.go
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./internal/redis ./internal/service ./internal/domain/auction
```

Expected: PASS。

- [ ] **Step 10: 提交**

```powershell
git add internal/domain/auction/model.go internal/redis/store.go internal/redis/store_test.go internal/service/auction.go internal/service/auction_test.go
git commit -m "feat: add realtime leaderboard snapshots"
```

## 任务 5：前端账号密码登录

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/LoginPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/session.ts`

- [ ] **Step 1: 更新 API 类型**

在 `web/src/api.ts` 中把 `User` 改为：

```ts
export type User = {
  id: string;
  username: string;
  displayName: string;
  role: Role;
  status: "active" | "disabled";
  createdAt?: string;
  updatedAt?: string;
};
```

把 `login` 改为：

```ts
export async function login(username: string, password: string): Promise<LoginSession> {
  return request("/api/login", undefined, {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
}
```

- [ ] **Step 2: 改登录页状态**

在 `web/src/LoginPage.tsx` 中删除 role segmented control，保留：

```tsx
const [username, setUsername] = useState(defaultRole === "admin" ? "admin" : "userA");
const [password, setPassword] = useState(defaultRole === "admin" ? "admin123" : "123456");
```

提交时调用：

```tsx
onLogin(await login(username, password));
```

表单 JSX：

```tsx
<label>
  用户名
  <input value={username} onChange={(event) => setUsername(event.target.value)} placeholder="admin / userA / userB / userC" />
</label>
<label>
  密码
  <input type="password" value={password} onChange={(event) => setPassword(event.target.value)} placeholder="输入密码" />
</label>
```

按钮 disabled 条件：

```tsx
disabled={submitting || !username.trim() || !password.trim()}
```

- [ ] **Step 3: App 显示名字段切换**

在 `web/src/App.tsx` 检查是否使用 `session.user.name`。如果有，改为：

```tsx
session.user.displayName
```

登录后仍按 `session.user.role` 跳转。

- [ ] **Step 4: session 文件不需要结构变化**

`web/src/session.ts` 继续保存 `LoginSession`。只检查没有依赖旧 `name` 字段。

- [ ] **Step 5: 构建验证**

Run:

```powershell
cd web
npm run build
```

Expected: PASS。

- [ ] **Step 6: 提交**

```powershell
git add web/src/api.ts web/src/LoginPage.tsx web/src/App.tsx web/src/session.ts
git commit -m "feat: use password login in frontend"
```

## 任务 6：前端 Top 5 排行榜与稳定身份显示

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/AdminApp.tsx`
- Modify: `web/src/MobileApp.tsx`
- Modify: `web/src/styles.css`

- [ ] **Step 1: 增加 leaderboard 类型**

在 `web/src/api.ts` 中增加：

```ts
export type LeaderboardEntry = {
  rank: number;
  userId: string;
  displayName: string;
  amount: number;
};
```

在 `Snapshot` 中增加：

```ts
leaderboard: LeaderboardEntry[];
```

- [ ] **Step 2: 管理端展示 Top 5**

在 `web/src/AdminApp.tsx` 增加组件：

```tsx
function Leaderboard({ items }: { items: LeaderboardEntry[] }) {
  return (
    <section className="leaderboard">
      <h3>Top 5 排行榜</h3>
      {items.length === 0 ? (
        <p className="empty-log">暂无出价</p>
      ) : items.map((item) => (
        <p key={item.userId}>
          <span>#{item.rank}</span>
          <strong>{item.displayName || item.userId}</strong>
          <em>{currency(item.amount)}</em>
        </p>
      ))}
    </section>
  );
}
```

在 `lot-panel` 中 `EventLog` 前加入：

```tsx
<Leaderboard items={live?.leaderboard ?? []} />
```

最高出价人显示从 `highestBidder` 改为：

```tsx
{live?.leaderboard?.[0]?.displayName || selected.highestBidder || "-"}
```

- [ ] **Step 3: 用户端展示 Top 5**

在 `web/src/MobileApp.tsx` 增加同名 `Leaderboard` 组件，放在 `mobile-metrics` 后：

```tsx
<Leaderboard items={snapshot?.leaderboard ?? []} currentUserId={session.user.id} />
```

组件中给当前用户行加 class：

```tsx
className={item.userId === currentUserId ? "mine" : ""}
```

- [ ] **Step 4: 我的排名基于 snapshot.rank**

保持：

```tsx
<Metric label="我的排名" value={snapshot?.rank ? `第 ${snapshot.rank}` : "-"} />
```

确认不再从本地临时用户推导排名。

- [ ] **Step 5: 日志显示 displayName**

把 AdminApp 和 MobileApp 中的：

```ts
session.user.name
```

统一改成：

```ts
session.user.displayName
```

事件日志优先使用：

```ts
event.meta?.bidderName || event.meta?.actorName
```

- [ ] **Step 6: 增加样式**

在 `web/src/styles.css` 增加：

```css
.leaderboard {
  display: grid;
  gap: 8px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: white;
  padding: 12px;
}

.leaderboard h3 {
  margin: 0 0 4px;
  font-size: 16px;
}

.leaderboard p {
  display: grid;
  grid-template-columns: 42px 1fr auto;
  align-items: center;
  gap: 8px;
  margin: 0;
  border-radius: 6px;
  background: #f5f1e8;
  padding: 9px 10px;
}

.leaderboard p.mine {
  outline: 2px solid var(--green);
  background: #edf8f1;
}

.leaderboard span {
  color: var(--muted);
  font-weight: 800;
}

.leaderboard strong,
.leaderboard em {
  font-style: normal;
}

.leaderboard em {
  color: var(--green);
  font-weight: 900;
}
```

- [ ] **Step 7: 构建验证**

Run:

```powershell
cd web
npm run build
```

Expected: PASS。

- [ ] **Step 8: 提交**

```powershell
git add web/src/api.ts web/src/AdminApp.tsx web/src/MobileApp.tsx web/src/styles.css
git commit -m "feat: show realtime top leaderboard"
```

## 任务 7：端到端测试覆盖多用户 JWT 和排行榜恢复

**Files:**
- Modify: `web/tests/realtime-ranking.spec.ts`

- [ ] **Step 1: 改登录 helper**

在 `web/tests/realtime-ranking.spec.ts` 顶部增加：

```ts
async function login(page: Page, username: string, password: string) {
  await page.getByLabel("用户名").fill(username);
  await page.getByLabel("密码").fill(password);
  await page.getByRole("button", { name: "进入" }).click();
}
```

同时 import：

```ts
import { expect, test, type Page } from "@playwright/test";
```

- [ ] **Step 2: 改核心 e2e 用例**

把现有用例改成三个独立 context：

```ts
test("JWT 多用户出价后两端实时刷新 Top 5，刷新后身份稳定", async ({ browser }) => {
  const productName = `星河手镯 ${Date.now()}`;
  const adminContext = await browser.newContext();
  const userAContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const userBContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const admin = await adminContext.newPage();
  const userA = await userAContext.newPage();
  const userB = await userBContext.newPage();

  await admin.goto("/admin");
  await login(admin, "admin", "admin123");
  await expect(admin.getByRole("heading", { name: "竞拍管理" })).toBeVisible();

  await admin.getByLabel("商品名称").fill(productName);
  await admin.getByLabel("起拍价").fill("0");
  await admin.getByLabel("加价幅度").fill("100");
  await admin.getByLabel("竞拍时长(秒)").fill("120");
  await admin.getByLabel("封顶价").fill("500");
  await admin.getByLabel("延时窗口(秒)").fill("20");
  await admin.getByLabel("延长时长(秒)").fill("30");
  await admin.getByRole("button", { name: "创建竞拍" }).click();
  await expect(admin.getByRole("button", { name: new RegExp(productName) })).toBeVisible();
  await admin.getByRole("button", { name: "启动" }).click();
  await expect(admin.getByText("RUNNING").first()).toBeVisible();

  await userA.goto("/m");
  await login(userA, "userA", "123456");
  await userA.getByRole("button", { name: new RegExp(productName) }).click();
  await userA.locator(".bid-dock input").fill("100");
  await userA.getByRole("button", { name: "出价" }).click();
  await expect(userA.getByText("用户A")).toBeVisible();
  await expect(userA.getByText("￥100")).toBeVisible();

  await userB.goto("/m");
  await login(userB, "userB", "123456");
  await userB.getByRole("button", { name: new RegExp(productName) }).click();
  await userB.locator(".bid-dock input").fill("200");
  await userB.getByRole("button", { name: "出价" }).click();

  await expect(admin.getByText("用户B").first()).toBeVisible();
  await expect(admin.getByText("￥200").first()).toBeVisible();
  await expect(userA.getByText("用户B").first()).toBeVisible();
  await expect(userA.getByText("第 2")).toBeVisible();

  await userA.reload();
  await expect(userA.getByText("用户A").first()).toBeVisible();
  await expect(userA.getByText("第 2")).toBeVisible();

  await userB.locator(".bid-dock input").fill("500");
  await userB.getByRole("button", { name: "出价" }).click();
  await expect(admin.getByText("SOLD").first()).toBeVisible();
  await expect(userA.getByText("SOLD").first()).toBeVisible();

  await adminContext.close();
  await userAContext.close();
  await userBContext.close();
});
```

- [ ] **Step 3: 运行 e2e 确认通过**

Run:

```powershell
cd web
npm run test:e2e
```

Expected: PASS。

- [ ] **Step 4: 提交**

```powershell
git add web/tests/realtime-ranking.spec.ts
git commit -m "test: cover jwt users and leaderboard"
```

## 任务 8：全量验收和本地服务重启

**Files:**
- Modify only if verification exposes a defect.

- [ ] **Step 1: 全量 Go 测试**

Run:

```powershell
$env:GOCACHE = (Resolve-Path .).Path + '\.gocache'
go test -count=1 ./...
```

Expected: PASS。

- [ ] **Step 2: 前端构建**

Run:

```powershell
cd web
npm run build
```

Expected: PASS。

- [ ] **Step 3: 端到端测试**

Run:

```powershell
cd web
npm run test:e2e
```

Expected: PASS。

- [ ] **Step 4: 重启本地后端和前端**

停止 8080 和 5173 监听进程：

```powershell
$ports = @(8080,5173)
foreach ($port in $ports) {
  netstat -ano | Select-String ":$port" | ForEach-Object {
    $parts = ($_ -split '\s+') | Where-Object { $_ }
    if ($parts.Length -ge 5 -and $parts[3] -eq 'LISTENING') {
      Stop-Process -Id ([int]$parts[4]) -Force -ErrorAction SilentlyContinue
    }
  }
}
```

启动后端：

```powershell
Start-Process -FilePath 'C:\Program Files\Go\bin\go.exe' -WindowStyle Hidden -WorkingDirectory (Get-Location).Path -ArgumentList 'run','./cmd/api'
```

启动前端：

```powershell
cd web
$env:VITE_API_BASE='http://127.0.0.1:8080'
Start-Process -FilePath 'C:\Program Files\nodejs\node.exe' -WindowStyle Hidden -WorkingDirectory (Get-Location).Path -ArgumentList 'node_modules\vite\bin\vite.js','--host','0.0.0.0','--port','5173'
```

- [ ] **Step 5: 冒烟验证**

Run:

```powershell
Invoke-RestMethod -Uri http://127.0.0.1:8080/healthz
(Invoke-WebRequest -Uri http://127.0.0.1:5173/admin).StatusCode
```

Expected:

```text
status ok
200
```

- [ ] **Step 6: 如果无额外修复，不创建提交**

Run:

```powershell
git status --short
```

Expected: clean。

如果 Step 1-5 过程中产生必要修复：

```powershell
git add <changed-files>
git commit -m "fix: stabilize jwt user ranking"
```

## 自查记录

- Spec 覆盖：预置账号、JWT 登录、用户表、废弃 session、Top 5 排行榜、刷新恢复、日志 displayName、Go/Playwright 测试均有对应任务。
- 类型一致：`User.Username`、`User.DisplayName`、`User.Status`、`Snapshot.Leaderboard`、`LeaderboardEntry.DisplayName` 在后端和前端任务中保持一致。
- 范围控制：不包含注册、用户管理页、PostgreSQL、真实 Redis、第三方密码哈希库。
