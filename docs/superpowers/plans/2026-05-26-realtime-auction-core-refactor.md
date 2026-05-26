# 实时竞拍核心链路重构实施计划

> **给执行 Agent：** 必须使用 `superpowers:subagent-driven-development`（推荐）或 `superpowers:executing-plans` 逐任务执行。任务使用复选框跟踪。

**目标：** 将现有实时竞拍 demo 重构为可演示的核心链路：演示登录、PC 管理端、移动 H5 用户端、本地文件持久化、自动结算、WebSocket 状态恢复。

**架构：** 保留 Go 标准库后端和 React/Vite 前端。后端以领域状态机、service 编排、repository 接口、JSON 文件存储分层；前端拆分为 `/login`、`/admin`、`/m` 三个入口。

**技术栈：** Go、React、TypeScript、Vite、Playwright、本地 JSON 文件、WebSocket。

---

## 文件边界

- `internal/domain/auction/model.go`：状态机、规则校验、用户和会话模型。
- `internal/repository/repository.go`：竞拍、出价、订单、用户、会话的持久化接口。
- `internal/repository/memory.go`：测试用内存实现。
- `internal/repository/file.go`：本地 JSON 文件实现。
- `internal/redis/store.go`：实时出价原子状态，延时作为事件，不再作为状态。
- `internal/service/auth.go`：演示登录与角色校验。
- `internal/service/auction.go`：竞拍创建、启动、出价、取消、自动结算、广播。
- `internal/http/server.go`：认证中间件、管理端 API、用户端 API、结构化错误。
- `cmd/api/main.go`：装配文件 repository、实时 store、WebSocket hub。
- `web/src/api.ts`：前端 API client、token、WebSocket 连接。
- `web/src/session.ts`：浏览器登录态。
- `web/src/App.tsx`：前端入口分流。
- `web/src/LoginPage.tsx`：演示登录页。
- `web/src/AdminApp.tsx`：PC 管理端。
- `web/src/MobileApp.tsx`：移动 H5 用户端。
- `web/src/styles.css`：双端样式。
- `web/tests/realtime-ranking.spec.ts`：端到端核心链路测试。

## 任务 1：收敛领域状态机

**文件：**
- 修改：`internal/domain/auction/model.go`
- 修改：`internal/domain/auction/model_test.go`

- [ ] **步骤 1：先写失败测试**

在 `internal/domain/auction/model_test.go` 增加测试：新竞拍只能从 `DRAFT` 启动到 `RUNNING`，`DRAFT/RUNNING` 可取消，时间到时有最高出价进入 `SOLD`，没有出价进入 `ENDED`。

```go
func TestAuctionStateMachineAllowsOnlyCoreTransitions(t *testing.T) {
	a := mustAuction(t)
	if a.Status != StatusDraft {
		t.Fatalf("new status = %s, want DRAFT", a.Status)
	}
	if err := a.Start(time.Now().UTC()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if a.Status != StatusRunning {
		t.Fatalf("status = %s, want RUNNING", a.Status)
	}
	if err := a.Cancel(); err != nil {
		t.Fatalf("cancel running: %v", err)
	}
	if a.Status != StatusCancelled {
		t.Fatalf("status = %s, want CANCELLED", a.Status)
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/domain/auction
```

预期：如果代码仍依赖 `SCHEDULED/EXTENDED` 状态，测试或编译会失败。

- [ ] **步骤 3：实现最小状态机**

`model.go` 只保留：

```go
const (
	StatusDraft     Status = "DRAFT"
	StatusRunning   Status = "RUNNING"
	StatusSold      Status = "SOLD"
	StatusEnded     Status = "ENDED"
	StatusCancelled Status = "CANCELLED"
)
```

`Start` 只允许 `DRAFT -> RUNNING`，`Cancel` 只允许 `DRAFT/RUNNING -> CANCELLED`，`IsOpenForBid` 只接受 `RUNNING`。

- [ ] **步骤 4：验证并提交**

```powershell
go test ./internal/domain/auction
git add internal/domain/auction/model.go internal/domain/auction/model_test.go
git commit -m "refactor: simplify auction state machine"
```

## 任务 2：增加本地文件 repository

**文件：**
- 修改：`internal/domain/auction/model.go`
- 修改：`internal/repository/repository.go`
- 修改：`internal/repository/memory.go`
- 新建：`internal/repository/file.go`
- 新建：`internal/repository/file_test.go`

- [ ] **步骤 1：先写失败测试**

`file_test.go` 覆盖：保存用户、session、竞拍、出价、订单后，重新打开 repository 可以读回；同一竞拍重复 `UpsertOrder` 返回第一次订单。

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/repository
```

预期：`NewFileRepository` 未定义。

- [ ] **步骤 3：增加用户和会话模型**

在 `model.go` 增加：

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
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
```

- [ ] **步骤 4：扩展 repository 接口**

增加 `SaveUser`、`SaveSession`、`GetUserByToken`。

- [ ] **步骤 5：实现文件存储**

`FileRepository` 使用 mutex 保护单进程写入，数据文件为 `auctions.json`、`bids.json`、`orders.json`、`users.json`、`sessions.json`。写入时先写 `.tmp`，再 `os.Rename` 替换。

- [ ] **步骤 6：验证并提交**

```powershell
go test ./internal/repository ./internal/...
git add internal/domain/auction/model.go internal/repository/repository.go internal/repository/memory.go internal/repository/file.go internal/repository/file_test.go
git commit -m "feat: add file-backed repository"
```

## 任务 3：延时改为事件语义

**文件：**
- 修改：`internal/redis/store.go`
- 修改：`internal/redis/store_test.go`

- [ ] **步骤 1：先写失败测试**

测试临近结束窗口内出价会更新 `endsAt`，`BidResult.Extended == true`，但快照状态仍是 `RUNNING`。

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/redis
```

- [ ] **步骤 3：修改实时 store**

在延时分支只更新 `EndsAt` 和 `Extended`，不要写 `StatusExtended`。取消只允许 `DRAFT/RUNNING`。

- [ ] **步骤 4：验证并提交**

```powershell
go test ./internal/redis ./internal/domain/auction
git add internal/redis/store.go internal/redis/store_test.go
git commit -m "refactor: model extension as auction event"
```

## 任务 4：演示登录服务

**文件：**
- 新建：`internal/service/auth.go`
- 新建：`internal/service/auth_test.go`

- [ ] **步骤 1：先写失败测试**

测试 `Login("用户A", RoleBidder)` 返回 token 和用户；`Require(token, RoleBidder)` 成功；`Require(token, RoleAdmin)` 返回 forbidden。

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/service -run Auth
```

- [ ] **步骤 3：实现 AuthService**

`AuthService` 负责创建用户、生成随机 token、保存 session、按 token 校验角色。错误使用 `ErrUnauthorized` 和 `ErrForbidden`。

- [ ] **步骤 4：验证并提交**

```powershell
go test ./internal/service -run Auth
git add internal/service/auth.go internal/service/auth_test.go
git commit -m "feat: add demo auth service"
```

## 任务 5：自动结算

**文件：**
- 修改：`internal/service/auction.go`
- 修改：`internal/service/auction_test.go`

- [ ] **步骤 1：先写失败测试**

测试启动一个 20ms 的竞拍后，不调用管理端结束接口，等待后自动进入 `ENDED`；达到封顶价出价后立即 `SOLD` 并生成订单。

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/service
```

- [ ] **步骤 3：实现结算定时器**

`AuctionService` 增加 `timers map[string]*time.Timer`。启动竞拍后按 `endsAt` 调度 `FinishExpired`；自动延时后重置 timer；`SOLD/ENDED/CANCELLED` 后停止 timer。

- [ ] **步骤 4：验证并提交**

```powershell
go test ./internal/service ./internal/redis ./internal/domain/auction
git add internal/service/auction.go internal/service/auction_test.go
git commit -m "feat: settle auctions automatically"
```

## 任务 6：认证 API 和结构化错误

**文件：**
- 修改：`internal/http/server.go`
- 修改：`internal/http/server_test.go`
- 修改：`cmd/api/main.go`

- [ ] **步骤 1：先写失败测试**

测试：`POST /api/login` 成功；普通用户访问 `/api/admin/auctions` 返回 403；管理员创建并启动竞拍成功；低价或步长错误出价返回 `{code,message,details}`。

- [ ] **步骤 2：运行测试确认失败**

```powershell
go test ./internal/http
```

- [ ] **步骤 3：拆分 API**

新增 `/api/login`、`/api/admin/auctions`、`/api/admin/auctions/{id}/start`、`/api/admin/auctions/{id}/cancel`；用户端保留 `/api/auctions`、`/api/auctions/{id}/snapshot`、`/api/auctions/{id}/bids`、`/api/auctions/{id}/result`、`/ws/auctions/{id}`。

- [ ] **步骤 4：实现后端角色校验**

从 `Authorization: Bearer <token>` 获取 session。管理端接口要求 `admin`，出价要求 `bidder`。WebSocket 可通过 query token 认证。

- [ ] **步骤 5：验证并提交**

```powershell
go test ./internal/http ./internal/service ./internal/repository ./internal/redis ./internal/domain/auction
git add internal/http/server.go internal/http/server_test.go cmd/api/main.go
git commit -m "feat: add authenticated auction api"
```

## 任务 7：前端登录态和 API client

**文件：**
- 修改：`web/src/api.ts`
- 新建：`web/src/session.ts`

- [ ] **步骤 1：实现 session 工具**

`session.ts` 提供 `loadSession`、`saveSession`、`clearSession`，存储 key 为 `auction-demo-session`。

- [ ] **步骤 2：改造 API client**

`api.ts` 增加 `login`、`adminListAuctions`、`adminCreateAuction`、`adminStartAuction`、`adminCancelAuction`、`listAuctions`、`getSnapshot`、`placeBid`、`openAuctionSocket`。统一在请求头带 token。

- [ ] **步骤 3：运行构建确认后续页面缺失**

```powershell
cd web
npm run build
```

预期：可能因为页面还没拆分而失败，继续任务 8-10 后统一修复。

## 任务 8：登录页和入口分流

**文件：**
- 修改：`web/src/App.tsx`
- 新建：`web/src/LoginPage.tsx`

- [ ] **步骤 1：实现 `/login` 体验**

登录页包含昵称输入、`用户端/管理端` 分段按钮、提交按钮、错误提示。

- [ ] **步骤 2：实现入口分流**

`App` 读取当前 session；未登录显示 `LoginPage`；路径以 `/admin` 开头显示 `AdminApp`；其他路径显示 `MobileApp`。

- [ ] **步骤 3：运行构建确认剩余页面缺失**

```powershell
cd web
npm run build
```

预期：可能因 `AdminApp/MobileApp` 缺失失败，继续任务 9-10。

## 任务 9：PC 管理端

**文件：**
- 新建：`web/src/AdminApp.tsx`
- 修改：`web/src/styles.css`

- [ ] **步骤 1：实现单页控制台**

左侧竞拍列表，右侧发布表单和当前竞拍操作区。`DRAFT` 显示启动/取消；`RUNNING` 显示取消；终态只查看信息。不要提供常规“手动结束”按钮。

- [ ] **步骤 2：运行构建确认移动端缺失**

```powershell
cd web
npm run build
```

预期：可能因 `MobileApp` 缺失失败，继续任务 10。

## 任务 10：移动 H5 用户端

**文件：**
- 新建：`web/src/MobileApp.tsx`
- 修改：`web/src/styles.css`

- [ ] **步骤 1：实现商品详情型页面**

页面展示商品图、商品名称、介绍、当前价、倒计时、下一口最低价、我的排名、参与人数、底部固定出价栏。

- [ ] **步骤 2：接入 WebSocket 恢复**

进入竞拍详情先调用 snapshot；WebSocket 断开后显示“连接中断，正在恢复”，短延迟重连并再次拉取 snapshot。

- [ ] **步骤 3：验证并提交前端拆分**

```powershell
cd web
npm run build
git add web/src/api.ts web/src/session.ts web/src/App.tsx web/src/LoginPage.tsx web/src/AdminApp.tsx web/src/MobileApp.tsx web/src/styles.css
git commit -m "feat: split admin and mobile clients"
```

## 任务 11：端到端测试

**文件：**
- 修改：`web/tests/realtime-ranking.spec.ts`
- 修改：`web/playwright.config.ts`

- [ ] **步骤 1：改造 Playwright 用例**

覆盖：管理员登录 `/admin` 创建并启动竞拍；用户登录 `/m` 出价到封顶价；页面显示 `SOLD` 和成交反馈。

- [ ] **步骤 2：验证**

```powershell
go test ./...
cd web
npm run build
npm run test:e2e
```

如果缺少浏览器，运行：

```powershell
cmd /c npx playwright install chromium
```

- [ ] **步骤 3：提交**

```powershell
git add web/tests/realtime-ranking.spec.ts web/playwright.config.ts
git commit -m "test: cover realtime auction core flow"
```

## 任务 12：最终验收

**文件：**
- 只在发现缺陷时修改相关文件。

- [ ] **步骤 1：全量测试**

```powershell
go test ./...
cd web
npm run build
npm run test:e2e
```

- [ ] **步骤 2：手动冒烟**

启动后端：

```powershell
go run ./cmd/api
```

启动前端：

```powershell
cd web
npm run dev
```

访问：

```text
http://localhost:5173/admin
http://localhost:5173/m
```

验收点：管理员可登录、创建、启动、取消；用户可登录、查看竞拍、出价、看到成交；刷新后可恢复 snapshot；管理端没有常规手动结束按钮。

- [ ] **步骤 3：必要时提交修复**

```powershell
git add <changed-files>
git commit -m "fix: stabilize auction refactor"
```

如果没有修复，不创建空提交。
