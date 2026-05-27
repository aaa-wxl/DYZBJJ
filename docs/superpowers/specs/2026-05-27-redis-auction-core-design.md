# Redis 竞拍核心链路设计

## 背景

PRD 要求直播竞拍在高并发场景下保持出价一致、排名实时、WebSocket 稳定，并支持房间级隔离。当前实现已经完成核心闭环，但实时竞拍状态由进程内 `MemoryStore` 承载，业务数据由本地 JSON 文件保存。这个方案适合本地演示，不适合多实例、高频出价和大量在线用户。

本阶段目标是按 Docker Redis 方案把竞拍实时核心迁到真实 Redis。MySQL 只在 Docker Compose 中预留，不接入业务代码；用户、竞拍配置、出价流水和订单仍暂时使用现有文件仓储。

## 目标

- 用 Docker Compose 启动 Redis，并预留 MySQL 服务。
- 新增真实 `RedisStore`，实现现有 `redis.Store` 接口。
- 将出价校验、价格更新、排行榜、幂等和延时判断放到 Redis Lua 脚本中原子完成。
- 保持 `AuctionService`、HTTP API、前端调用方式基本不变。
- 保留 `MemoryStore` 用于单元测试和无 Redis 的快速逻辑测试。
- Redis 不可用时后端启动失败，不静默降级到内存实现。

## 非目标

- 本阶段不把用户、商品、竞拍配置、订单迁移到 MySQL。
- 不实现支付、订单后台、历史竞拍查询扩展。
- 不做真正的分布式部署脚本，只完成可横向扩展的 Redis 事件边界。
- 不引入复杂网关或消息队列，优先用 Redis Pub/Sub 满足当前演示。

## 架构

后端仍保持现有分层：

- `internal/http`：鉴权、REST API、WebSocket/SSE 推送。
- `internal/service`：竞拍应用服务、启动、取消、结算、审计写入。
- `internal/repository`：本地文件仓储，暂存业务事实。
- `internal/redis`：实时竞拍状态接口和实现。

新增 `internal/redis/redis_store.go`，实现：

- `InitAuction`
- `PlaceBid`
- `Snapshot`
- `Cancel`
- `FinishExpired`

`cmd/api/main.go` 启动时：

1. 读取 `REDIS_ADDR`，默认 `127.0.0.1:6379`。
2. 创建 Redis client。
3. `PING` Redis。
4. 成功后使用 `RedisStore`。
5. 失败则直接 `log.Fatal`，提示先启动 Docker Redis。

## Docker Compose

新增 `docker-compose.yml`：

- `redis:7-alpine`
  - 端口 `6379:6379`
  - 开启 AOF，降低演示环境重启丢状态风险。
- `mysql:8`
  - 端口 `3306:3306`
  - 设置本地演示账号和数据库。
  - 本阶段只预留，不被 Go 代码连接。

后端仍由本机启动：

```powershell
docker compose up -d redis mysql
$env:REDIS_ADDR="127.0.0.1:6379"
$env:HTTP_ADDR="127.0.0.1:8080"
go run ./cmd/api
```

## Redis Key 设计

以竞拍 ID 隔离房间数据：

- `auction:{id}:snapshot`：Hash，保存实时快照字段。
- `auction:{id}:ranking`：ZSet，按金额排序。
- `auction:{id}:rank_seq`：Hash，保存用户首次达到当前最高价的顺序号。
- `auction:{id}:seq`：String，自增序号，用于同价先到先得。
- `auction:{id}:request:{requestId}`：String，保存幂等返回结果 JSON，设置 TTL。
- `auction:{id}:events`：Pub/Sub channel，用于多实例事件传播。

`snapshot` 主要字段：

- `auctionId`
- `status`
- `currentPrice`
- `highestBidder`
- `endsAtUnixMs`
- `serverTimeUnixMs`
- `startPrice`
- `increment`
- `durationMs`
- `ceilingPrice`
- `extendThresholdMs`
- `extendByMs`
- `productJson`

## 出价原子流程

`RedisStore.PlaceBid` 调用 Lua 脚本。脚本一次性完成：

1. 检查 `requestId` 幂等 key。
2. 读取 `snapshot`。
3. 校验竞拍存在。
4. 校验状态必须是 `RUNNING`。
5. 校验当前时间不能晚于 `endsAt`。
6. 计算 `nextMinimumBid`。
7. 校验出价不低于最低价。
8. 校验出价符合加价幅度。
9. 更新当前价和最高出价人。
10. 维护用户最高出价。
11. 维护排行榜 ZSet。
12. 同价时用 `rank_seq` 保留先到顺序。
13. 达到封顶价则状态改为 `SOLD`。
14. 临近结束则延长 `endsAt`。
15. 写入幂等结果，并设置 TTL。
16. 返回最新 snapshot、Top 5、当前用户排名、事件类型。

金额排序用 ZSet score。为了支持同价先到先得，score 不能只用金额。设计为组合 score：

```text
score = amount * 1_000_000_000 - seq
```

金额越大 score 越高；金额相同，seq 越小 score 越高。金额单位为整数，演示场景不会超过该组合范围。

## Snapshot 与排行榜

`RedisStore.Snapshot(auctionID, userID)`：

- 读取 `snapshot` Hash。
- 读取 `ZREVRANGE ranking 0 4 WITHSCORES` 得到 Top 5。
- 读取 `ZREVRANK ranking userID` 得到当前用户排名。
- 使用 Hash 或 score 还原展示金额。
- 返回 `auction.Snapshot`。

`DisplayName` 仍由 `AuctionService.enrichLeaderboard` 通过 repository 补齐，Redis 只保存 userId 和金额。

## 事件同步

本阶段保留当前进程内 `ws.Hub`，同时新增 Redis Pub/Sub 边界：

- `RedisStore.PlaceBid` 成功后发布事件到 `auction:{id}:events`。
- 后端启动时启动 Redis 订阅器。
- 每个实例收到 Pub/Sub 事件后调用本机 `Hub.Broadcast`。
- 当前单实例演示仍可正常工作；多实例时，任一实例处理出价，其他实例的 WebSocket 用户也能收到事件。

事件体只携带：

- `type`
- `auctionId`
- `reason`
- `meta`

发送给具体连接前，HTTP 层继续使用 `personalizeEvent` 按连接用户重新读取 snapshot，保证“我的排名”是个性化的。

## 取消与结算

`Cancel` 使用 Lua 或 Redis 事务原子更新状态：

- 仅允许 `DRAFT` 和 `RUNNING` 取消。
- 更新 snapshot 状态为 `CANCELLED`。
- 发布 `auctionCancelled` 事件。

`FinishExpired` 使用 Lua 原子判断：

- 仅允许 `RUNNING`。
- 当前时间必须大于等于 `endsAt`。
- 有最高出价则 `SOLD`，否则 `ENDED`。
- 发布 `auctionEnded` 事件。

`AuctionService` 仍负责把终态同步回文件仓储，并生成订单。

## 幂等与过期

每次出价请求必须带 `requestId`。Redis 幂等 key：

```text
auction:{id}:request:{requestId}
```

TTL 建议 10 分钟。重复 requestId 返回第一次的 `BidResult`，不重复扣写排行榜和出价流水。

竞拍结束后，实时 key 可设置较长 TTL，例如 24 小时，便于结果页和演示回看。业务事实仍以文件仓储为准。

## 配置

新增或明确以下配置：

- `REDIS_ADDR`：默认 `127.0.0.1:6379`
- `REDIS_PASSWORD`：默认空
- `REDIS_DB`：默认 `0`
- `REDIS_REQUIRED`：默认 `true`

当前阶段默认 Redis 必须可用。这样演示时不会误以为使用了 Redis，实际却回退到了内存。

## 测试策略

单元测试：

- 保留 `MemoryStore` 现有规则测试。
- 增加 Redis Lua 脚本输入输出测试。
- 增加 `RedisStore` 集成测试，依赖本地 Docker Redis。

集成测试：

- 启动 Redis 后跑：
  - 高并发出价不会降价。
  - 同一个 requestId 幂等。
  - Top 5 排名正确。
  - 当前用户 rank 正确。
  - 封顶价立即 SOLD。
  - 临近结束自动延时。
  - 取消和结算状态转移正确。

E2E：

- 沿用现有 Playwright 多用户测试。
- 确认 userA/userB 不同浏览器上下文出价后，管理端和用户端实时同步。
- 刷新后身份和排名稳定。

## 风险与处理

- Redis Lua 脚本复杂度较高：脚本保持单文件、输入输出结构固定，并用测试覆盖核心分支。
- Redis Pub/Sub 不持久：本阶段用于实时通知即可；状态恢复依赖 `Snapshot` 读取 Redis。
- 本地 Docker 未启动：后端启动失败并提示 `docker compose up -d redis mysql`。
- 文件仓储仍是瓶颈：出价流水保存仍写 JSON，这是后续 MySQL 阶段要解决的问题。本阶段只保证实时出价链路由 Redis 承接。

## 验收标准

- `docker compose up -d redis mysql` 后，后端能连接 Redis 启动。
- Redis 未启动时，后端启动失败并给出明确错误。
- `go test -count=1 ./...` 通过。
- Redis 集成测试通过。
- `npm run build` 通过。
- `npm run test:e2e` 通过。
- 代码中真实出价路径使用 `RedisStore`，不再依赖 `MemoryStore` 承接运行时竞拍状态。
