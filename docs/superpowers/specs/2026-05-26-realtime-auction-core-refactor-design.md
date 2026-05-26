# 实时竞拍核心链路重构设计

日期：2026-05-26

## 目标

重构现有实时竞拍项目，先交付一条稳定、可演示的核心功能链路：演示登录、管理端发布并启动竞拍、用户端实时出价、自动延时、封顶成交、到点自动结算、订单或结果查看。第一版存储使用本地 JSON 文件，但通过 repository 边界保留后续切换 PostgreSQL 的空间。

## 范围

包含：

- Go 后端领域模型、状态机、服务编排、文件持久化、REST API、WebSocket 房间广播。
- React + TypeScript 前端拆分为登录页、PC 管理端、移动 H5 用户端。
- 演示登录支持不同用户和角色，后端校验管理端与用户端权限。
- 自动结算由系统触发，管理端不提供常规手动结束按钮。

不包含：

- 真实支付、真实直播流、账号密码注册、Redis/PostgreSQL 实现、复杂运营后台、大规模可观测平台。

## 后端架构

后端继续使用 Go 标准库 HTTP 服务，并按以下边界整理：

- `internal/domain/auction`：竞拍领域模型、规则校验、状态机转移和不可变量。
- `internal/service`：创建竞拍、启动竞拍、出价、自动延时、自动结算、取消、订单生成和结果查询。
- `internal/repository`：持久化接口。第一版实现文件 repository，负责读写 `data/users.json`、`data/sessions.json`、`data/auctions.json`、`data/bids.json`、`data/orders.json`。
- `internal/ws` 或等价实时模块：按 `auctionId` 维护房间订阅，广播竞拍事件。
- `internal/http`：REST API、WebSocket 入口、认证和角色校验。

文件 repository 需要保证单进程内写入互斥，避免同一竞拍并发出价或结算时写坏 JSON 文件。接口设计要让后续 PostgreSQL repository 可以替换，不把文件格式泄漏到 service 层。

## 状态机

第一版状态固定为：

```text
DRAFT -> RUNNING
DRAFT -> CANCELLED
RUNNING -> SOLD
RUNNING -> ENDED
RUNNING -> CANCELLED
```

状态含义：

- `DRAFT`：已创建但未开始。管理端可编辑规则、启动、取消。用户端可查看但不可出价。
- `RUNNING`：竞拍进行中。用户可出价，管理端可异常取消，系统可自动延时或结算。
- `SOLD`：已成交。达到封顶价，或到点结算时存在最高出价。进入后生成订单。
- `ENDED`：已结束但未成交。到点结算时没有有效出价。
- `CANCELLED`：管理端异常取消。取消后不允许出价，不生成订单。

`EXTENDED` 不作为独立状态，而是 `RUNNING` 下的事件 `AUCTION_EXTENDED`。延时只更新 `endsAt`，竞拍仍处于进行中。

不可变量：

- 只有 `RUNNING` 接受出价。
- `SOLD`、`ENDED`、`CANCELLED` 是终态，终态不可逆。
- 只有 `SOLD` 生成订单，且一个竞拍最多一个订单。
- `currentPrice` 只能由有效出价更新。
- `endsAt` 只能在启动竞拍或自动延时时更新。
- 封顶成交优先于自动延时。如果一笔出价同时处于延时窗口并达到封顶价，直接成交。

## 前端设计

前端继续使用 React + TypeScript + Vite，拆成三个入口：

- `/login`：演示登录页。用户输入昵称并选择角色 `admin` 或 `bidder`，登录后保存本地 session。
- `/admin`：PC 管理端单页控制台。左侧竞拍列表，右侧工作区。`DRAFT` 可编辑、启动、取消；`RUNNING` 可查看实时状态并取消；终态只查看结果或订单。
- `/m` 或 `/mobile`：移动 H5 用户端。用户登录后进入竞拍列表，选择竞拍进入商品详情页。

用户端采用移动 H5 商品详情型，而不是直播间型。详情页展示商品图、商品介绍、当前价、倒计时、下一口最低价、我的排名、排行榜、出价输入和底部固定出价按钮。

管理端视觉方向是简洁操作台：浅色背景、清晰表格和表单、状态优先、动作明确。用户端视觉方向是移动电商详情页：价格和倒计时优先，按钮醒目，避免复杂直播装饰。现有乱码文案统一修复为正常 UTF-8 中文。

## 数据流与接口

核心链路：

1. `POST /api/login`：创建演示 session，返回 `token` 和 `user`。
2. `POST /api/admin/auctions`：管理员创建竞拍，状态为 `DRAFT`。
3. `POST /api/admin/auctions/{id}/start`：管理员启动竞拍，状态变为 `RUNNING`，写入 `startsAt` 和 `endsAt`，启动自动结算任务，广播 `AUCTION_STARTED`。
4. `POST /api/auctions/{id}/bids`：普通用户出价。后端校验身份、状态、最低价、加价幅度、封顶价和 `requestId` 幂等。成功后写入 bid，更新快照并广播 `BID_ACCEPTED`。如果进入延时窗口，更新 `endsAt` 并广播 `AUCTION_EXTENDED`。如果达到封顶价，进入 `SOLD` 并生成订单。
5. 系统到点执行 `finishExpiredAuction(id)`：有最高出价则 `RUNNING -> SOLD` 并生成订单，没有出价则 `RUNNING -> ENDED`，随后广播结果事件。

查询接口：

- `GET /api/auctions`：用户端竞拍列表。
- `GET /api/admin/auctions`：管理端竞拍列表，包含更多管理字段。
- `GET /api/auctions/{id}/snapshot`：用户端入房或重连后的状态恢复。
- `GET /api/auctions/{id}/result`：查看成交、流拍或取消结果。
- `GET /ws/auctions/{id}`：订阅竞拍房间实时事件。

所有需要身份的请求带 `Authorization: Bearer <token>`。后端按 session 查用户和角色，不能只依赖前端隐藏按钮。

## 错误处理

后端统一返回结构：

```json
{
  "code": "BID_TOO_LOW",
  "message": "出价低于最低有效价",
  "details": {
    "nextMinimumBid": 1200
  }
}
```

核心错误码：

- `UNAUTHORIZED`：未登录或 token 失效。
- `FORBIDDEN`：角色不允许操作。
- `INVALID_STATE`：当前状态不允许操作。
- `BID_TOO_LOW`：出价低于下一口最低价。
- `BID_STEP_INVALID`：不符合加价幅度。
- `DUPLICATE_REQUEST`：重复 `requestId`。
- `AUCTION_NOT_FOUND`：竞拍不存在。

前端用户端在底部出价栏附近展示出价错误，重点提示最低有效价、竞拍已结束和连接恢复状态。管理端在工作区顶部展示创建、启动、取消等操作错误。WebSocket 断开时自动重连，并调用 snapshot 恢复状态。

## 测试

Go 测试覆盖：

- 状态机和规则校验。
- 出价校验、封顶成交、自动延时。
- 自动结算、异常取消、订单幂等。
- 文件 repository 读写、重启后数据恢复、重复订单保护。
- HTTP 登录、权限、管理接口和用户出价接口。

前端测试覆盖一条核心 Playwright 链路：管理员登录创建并启动竞拍，两个普通用户分别登录出价，检查当前价、排行榜和成交结果更新。

## 实施顺序

1. 整理领域模型和状态机，移除 `EXTENDED` 状态语义，保留延时事件。
2. 增加演示登录和角色校验。
3. 实现文件 repository，并让 service 只依赖 repository 接口。
4. 调整 API 路由和错误结构。
5. 拆分前端 `/login`、`/admin`、`/m`。
6. 接入 WebSocket 重连和 snapshot 恢复。
7. 补充后端单元测试、HTTP 测试和前端核心链路测试。
