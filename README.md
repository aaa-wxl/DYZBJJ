# 实时竞拍大师

这是 `realtime-auction-core` 的最小实现骨架，目标是打通直播竞拍核心闭环：商家创建竞拍、启动竞拍、用户进入房间、实时出价、房间广播、成交后生成最小订单。

## 技术栈

- 后端：Go 标准库 HTTP 服务
- 前端：React + TypeScript + Vite
- 热点状态：`internal/redis` 中的内存原子 store，接口按 Redis 原子出价模型设计，后续可替换为真实 Redis Lua 脚本
- 数据库：提供 `migrations/001_init.sql`，可用于 MySQL/PostgreSQL 风格的本地建表调整
- 实时通信：`/ws/auctions/{id}` 提供最小 WebSocket 推送，`/api/auctions/{id}/events` 提供 SSE 兜底

## 本地启动

1. 准备配置：

```bash
cp .env.example .env
```

2. 启动后端：

```bash
go run ./cmd/api
```

3. 启动前端：

```bash
cd web
npm install
npm run dev
```

4. 打开页面：

```text
http://localhost:5173
```

## 核心 API

- `POST /api/auctions`：创建竞拍
- `GET /api/auctions`：查看竞拍列表
- `POST /api/auctions/{id}/start`：启动竞拍
- `POST /api/auctions/{id}/cancel`：取消竞拍
- `GET /api/auctions/{id}/snapshot?userId=...`：获取竞拍快照
- `POST /api/auctions/{id}/bids`：提交出价
- `POST /api/auctions/{id}/finish`：处理自然结束
- `GET /api/auctions/{id}/result`：查看竞拍结果
- `GET /ws/auctions/{id}?userId=...`：订阅竞拍 WebSocket 事件

## 手工验证清单

- 创建竞拍：填写商品名称、起拍价、加价幅度、竞拍时长、封顶价和延时规则后创建成功
- 启动竞拍：`DRAFT` 竞拍启动后进入 `RUNNING`
- 用户入房：用户端选择竞拍后能看到当前价、倒计时和下一次最低出价
- 有效出价：符合加价幅度的出价被接受，当前价和排名更新
- 低价出价：低于最低有效出价时返回错误和 `nextMinimum`
- 自动延时：结束前窗口内有效出价在 `RUNNING` 中更新 `endsAt` 并广播延时事件
- 封顶成交：达到封顶价后状态进入 `SOLD` 并生成订单
- 自然结束：有最高出价时进入 `SOLD`，无有效出价时进入 `ENDED`
- 取消竞拍：运行中竞拍可取消并广播 `CANCELLED`
- 重连恢复：刷新用户页面后重新获取最新快照
