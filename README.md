# 实时竞拍大师

实时竞拍大师是一个面向抖音电商直播竞拍场景的全栈 Demo，目标是打通“竞拍发布 -> 用户入房 -> 实时出价 -> 排行榜同步 -> 自动延时 / 封顶成交 -> 订单生成”的核心闭环。

## 技术栈

- 后端：Go 标准库 HTTP 服务
- 前端：React + TypeScript + Vite
- 实时状态：Redis 7 + Lua 脚本，承载原子出价、幂等、排行榜、自动延时和终态流转
- 持久化：本地 `data/*.json` 保存用户、竞拍配置、出价流水和订单；`migrations/001_init.sql` 预留 MySQL 表结构
- 实时通信：WebSocket 房间推送，SSE 作为兜底事件流，Redis Pub/Sub 预留多实例事件同步边界
- 自动化验证：Go 单元测试 / Redis 集成测试 / Playwright 多用户 E2E

## 功能概览

- 管理端发布竞拍，配置商品信息、起拍价、加价幅度、竞拍时长、封顶价和自动延时规则
- 管理端启动、取消竞拍，并实时查看当前价、最高出价人、参与人数和 Top 5 排行榜
- 用户端登录后进入移动端竞拍房间，查看倒计时、最低有效出价、个人排名和实时日志
- 后端通过 Redis Lua 原子校验出价，避免并发下价格回退、重复请求和排行榜错乱
- 达到封顶价后立即成交，到点自动结算；成交后生成 `PENDING_PAYMENT` 订单
- WebSocket 断开后前端自动重连，并通过 snapshot 恢复最新竞拍状态

## 本地启动

### 1. 启动依赖

```powershell
docker compose up -d redis mysql
```

当前阶段 Redis 承载实时竞拍状态；MySQL 只在 Docker Compose 中预留，业务数据仍写入 `data/*.json`。

### 2. 启动后端

```powershell
$env:REDIS_ADDR="127.0.0.1:6379"
$env:HTTP_ADDR="127.0.0.1:8080"
go run ./cmd/api
```

健康检查：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
```

### 3. 启动前端

```powershell
cd web
npm install
$env:VITE_API_BASE="http://127.0.0.1:8080"
npm run dev -- --host 127.0.0.1 --port 5173
```

访问入口：

- 管理端：`http://127.0.0.1:5173/admin`
- 用户端：`http://127.0.0.1:5173/m`

## 体验账号

- 管理员：`admin / admin123`
- 用户 A：`userA / 123456`
- 用户 B：`userB / 123456`
- 用户 C：`userC / 123456`

## 核心 API

- `POST /api/login`：登录并获取 JWT
- `GET /api/admin/auctions`：管理端查看竞拍列表
- `POST /api/admin/auctions`：管理端创建竞拍
- `POST /api/admin/auctions/{id}/start`：启动竞拍
- `POST /api/admin/auctions/{id}/cancel`：取消竞拍
- `GET /api/auctions`：用户端查看竞拍列表
- `GET /api/auctions/{id}/snapshot`：获取竞拍实时快照
- `POST /api/auctions/{id}/bids`：提交出价
- `GET /api/auctions/{id}/result`：查看竞拍结果和订单
- `GET /api/auctions/{id}/events`：订阅 SSE 事件流
- `GET /ws/auctions/{id}?token=...`：订阅 WebSocket 事件

## 手工验证清单

- 创建竞拍：填写商品名称、起拍价、加价幅度、竞拍时长、封顶价和延时规则后创建成功
- 启动竞拍：`DRAFT` 竞拍启动后进入 `RUNNING`
- 用户入房：用户端选择竞拍后能看到当前价、倒计时和下一次最低出价
- 有效出价：符合加价幅度的出价被接受，当前价和排名更新
- 低价出价：低于最低有效出价时返回错误和 `nextMinimumBid`
- 自动延时：结束前窗口内有效出价在 `RUNNING` 中更新 `endsAt` 并广播延时事件
- 封顶成交：达到封顶价后状态进入 `SOLD` 并生成订单
- 自然结束：有最高出价时进入 `SOLD`，无有效出价时进入 `ENDED`
- 取消竞拍：运行中竞拍可取消并广播 `CANCELLED`
- 重连恢复：刷新用户页面后重新获取最新 snapshot、排行榜和个人排名

## 测试

后端全量测试：

```powershell
go test -count=1 ./...
```

前端构建：

```powershell
cd web
npm run build
```

端到端测试：

```powershell
docker compose up -d redis mysql
cd web
npm run test:e2e
```

Redis 集成测试：

```powershell
docker compose up -d redis mysql
$env:REDIS_INTEGRATION="1"
$env:REDIS_ADDR="127.0.0.1:6379"
go test -count=1 ./internal/redis
```

## 目录结构

```text
cmd/api/              后端入口
internal/config/      配置读取
internal/domain/      竞拍领域模型和状态机
internal/http/        HTTP API、鉴权入口、WebSocket/SSE
internal/redis/       Redis Store、Lua 脚本、实时状态接口
internal/repository/  文件仓储
internal/service/     竞拍编排、结算和鉴权服务
internal/ws/          房间 Hub 和 Redis Pub/Sub 事件桥
migrations/           MySQL 表结构预留
web/                  React + TypeScript 前端
docs/                 设计文档和实现计划
```

