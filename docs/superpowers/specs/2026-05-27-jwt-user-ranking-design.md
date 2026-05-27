# JWT 用户体系与实时排行榜设计

日期：2026-05-27

## 背景

当前登录仍是演示模式：前端传昵称和角色，后端每次登录创建一个临时用户和 session。这个模型会导致同一个浏览器内切换身份后覆盖本地登录态，也无法稳定表达真实用户。用户 A 刷新后看起来变成用户 B，本质上是登录态和用户身份都不稳定。

本设计把登录改成真实账号密码模型，使用预置账号、稳定用户 ID、JWT token 鉴权，并在竞拍快照里增加 Top 5 实时排行榜。

## 目标

- 用户端使用真实账号密码登录。
- 每个用户拥有稳定 `userId`，刷新页面后身份不变。
- 同一个浏览器只保存一个当前账号；再次登录其他账号视为主动切换。
- 后端使用 JWT 鉴权，不再依赖 session 表。
- 管理端和用户端都能实时看到 Top 5 排行榜。
- 出价、日志、排行榜都使用稳定 `userId` 和 `displayName`。

## 不做范围

- 不做自助注册。
- 不做管理端用户管理页面。
- 不做密码重置、验证码、多因子登录。
- 不引入 PostgreSQL 或 Redis 真服务。
- 不引入第三方密码哈希库；第一版使用标准库哈希，后续可升级。

## 预置账号

后端启动时 seed 以下账号：

| username | password | role | displayName | userId |
|---|---|---|---|---|
| admin | admin123 | admin | 管理员 | usr-admin |
| userA | 123456 | bidder | 用户A | usr-user-a |
| userB | 123456 | bidder | 用户B | usr-user-b |
| userC | 123456 | bidder | 用户C | usr-user-c |

seed 行为：

- 启动时调用 `SeedDemoUsers()`。
- 用户不存在时创建。
- 用户存在时允许更新 `displayName`、`role`、`status` 和密码哈希，保证 demo 账号可恢复。
- 不清空竞拍、出价、订单数据。

## 用户模型

`User` 增加字段：

```go
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	PasswordHash string    `json:"-"`
	PasswordSalt string    `json:"-"`
	Role         Role      `json:"role"`
	Status       UserStatus `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
```

`UserStatus` 第一版只需要：

```go
const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)
```

本地文件 `data/users.json` 以 `userId` 为 key。`username` 必须唯一。

## 密码哈希

第一版使用标准库：

```text
passwordHash = sha256(passwordSalt + password)
```

原因：

- 当前阶段避免引入第三方依赖。
- 字段设计保留 `passwordHash/passwordSalt`，后续可替换为 bcrypt 或 argon2。
- API 和 repository 边界不暴露哈希算法。

## JWT 鉴权

登录接口改为：

```http
POST /api/login
```

请求：

```json
{
  "username": "userA",
  "password": "123456"
}
```

响应：

```json
{
  "token": "...jwt...",
  "user": {
    "id": "usr-user-a",
    "username": "userA",
    "displayName": "用户A",
    "role": "bidder",
    "status": "active"
  }
}
```

JWT payload：

```json
{
  "sub": "usr-user-a",
  "username": "userA",
  "role": "bidder",
  "iat": 1779840000,
  "exp": 1779926400
}
```

签名：

- 使用 HMAC-SHA256。
- secret 从 `JWT_SECRET` 环境变量读取。
- 本地默认值允许使用固定 demo secret。
- token 有效期第一版为 24 小时。

鉴权流程：

1. 从 `Authorization: Bearer <jwt>` 或 WebSocket query `token` 读取 token。
2. 校验 JWT 签名。
3. 校验 `exp`。
4. 从 `sub` 读取 `userId`。
5. `repo.GetUser(userId)`。
6. 校验用户存在且 `status == active`。
7. 校验接口所需 role。

WebSocket 继续使用：

```text
/ws/auctions/{id}?token=<jwt>
```

## Repository 边界

增加或调整接口：

```go
UpsertUser(user auction.User) error
GetUser(id string) (auction.User, error)
GetUserByUsername(username string) (auction.User, error)
ListUsers() ([]auction.User, error)
```

废弃 session 相关接口：

- `SaveSession`
- `GetUserByToken`

为了降低一次性改动风险，可以先保留旧接口但不再由 AuthService 使用，确认无依赖后再删除。

## AuthService 边界

AuthService 负责：

- `SeedDemoUsers()`
- `Login(username, password)`
- `Require(token, role)`
- `RequireAny(token)`

`Login`：

1. trim `username`。
2. 按 `username` 查用户。
3. 校验用户 active。
4. 校验密码 hash。
5. 签发 JWT。
6. 返回 token 和用户 DTO。

`Require`：

1. 验证 JWT。
2. 查用户表。
3. 校验 active。
4. 校验 role。

## 前端登录态

前端登录页改成：

- 用户名输入框。
- 密码输入框。
- 不再选择角色。
- 成功后按后端返回的 `user.role` 跳转：
  - `admin` -> `/admin`
  - `bidder` -> `/m`

本地仍使用：

```text
localStorage["auction-demo-session"]
```

但内容变为 JWT 登录结果。刷新页面时只复用 token，不重新创建用户。

同一浏览器内再次登录其他账号视为明确切换账号。若要同时模拟 userA/userB，需要使用两个浏览器、无痕窗口或 Playwright 双上下文。

## 实时排行榜

`Snapshot` 增加 `leaderboard`：

```go
type LeaderboardEntry struct {
	Rank        int    `json:"rank"`
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Amount      int64  `json:"amount"`
}
```

```go
type Snapshot struct {
	...
	Rank        int                `json:"rank,omitempty"`
	Leaderboard []LeaderboardEntry `json:"leaderboard"`
}
```

排行榜规则：

- 显示 Top 5。
- 每个用户只取最高有效出价。
- 金额高排前。
- 金额相同，先达到该金额者排前。
- 当前用户 `rank` 仍保留在 snapshot 中，基于 JWT 用户计算。

实时 store 需要在排名结构里记录用户最高价的更新时间或递增序号，保证同金额先到先得。

## 出价数据流

1. 前端带 JWT 调用 `POST /api/auctions/{id}/bids`。
2. 后端通过 JWT 得到稳定 `userId`。
3. 后端忽略前端传入的任何 `userId`。
4. store 更新当前价、用户最高价、排名序号。
5. service 根据排名里的 `userId` 查用户 `displayName`。
6. 广播包含 `leaderboard` 的 snapshot。
7. 管理端和用户端同步更新价格、状态、倒计时、我的排名、Top 5。

## 日志显示

事件日志显示稳定用户名称：

- 出价：`用户A 出价 ￥500`
- 启动：`管理员 开始了竞拍`
- 取消：`管理员 取消了竞拍`
- 成交：`达到封顶价，竞拍结束 ￥3000`
- 到点：`时间到，竞拍结束`

事件里可以携带当时的 `displayName`，避免用户后续改名或停用影响历史日志可读性。

## 错误处理

新增或明确错误码：

- `INVALID_CREDENTIALS`：账号或密码错误。
- `USER_DISABLED`：用户被禁用。
- `TOKEN_EXPIRED`：JWT 已过期。
- `UNAUTHORIZED`：未登录或 token 不合法。
- `FORBIDDEN`：角色无权操作。

登录失败不区分“用户名不存在”和“密码错误”，统一返回 `INVALID_CREDENTIALS`。

## 测试

Go 测试：

- seed 预置账号。
- username 唯一查询。
- 正确账号密码登录成功。
- 错误密码登录失败。
- disabled 用户不可登录。
- JWT 签名错误失败。
- JWT 过期失败。
- bidder 不能访问 admin API。
- 出价使用 JWT 的 userId，而不是请求体 userId。
- leaderboard Top 5 排序、同价先到先得、当前用户 rank。

Playwright 测试：

- admin 登录创建并启动竞拍。
- userA 登录出价。
- userB 在独立 browser context 登录出价。
- 两端排行榜实时刷新。
- 刷新 userA 页面后仍为 userA，排名恢复。
- 管理端无需手动刷新即可看到最高价、状态、Top 5 和结束事件。

## 迁移说明

现有 `data/sessions.json` 不再使用，可以保留文件但不读取。现有临时用户如果缺少 `username/passwordHash/status`，不参与新登录流程。预置账号 seed 会确保 demo 账号可用。

后续切 PostgreSQL 时，对应表：

- `users`
- `auctions`
- `bids`
- `orders`

JWT 鉴权逻辑不依赖存储类型，只依赖 user repository 查询。
