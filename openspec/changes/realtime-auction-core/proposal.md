## Why

《实时竞拍大师》的核心价值在于让直播间竞拍在高并发出价、实时同步和复杂规则约束下仍然保持一致、可用、可演示。第一个 change 需要先打通最小但完整的业务闭环，为后续管理端完善、压测优化和体验打磨提供稳定基础。

## What Changes

- 新增商家创建竞拍商品并配置规则的能力，包括起拍价、加价幅度、竞拍时长、封顶价和自动延时规则。
- 新增竞拍状态机，覆盖 `DRAFT`、`SCHEDULED`、`RUNNING`、`EXTENDED`、`SOLD`、`ENDED`、`CANCELLED`。
- 新增用户进入竞拍房间、订阅竞拍状态和实时出价的核心流程。
- 新增 Go 后端的出价校验能力，校验竞拍状态、起拍价、加价幅度、封顶价和自动延时。
- 新增 Redis 高频竞拍状态存储和原子更新机制，保证并发出价下的价格、排名和结束状态一致。
- 新增 WebSocket 房间广播能力，按 `auctionId` 推送当前价、倒计时、排名变化和竞拍结束事件。
- 新增成交后的最小订单记录，确保从商品上架到竞拍成交形成闭环。
- 暂不实现真实支付、复杂 AI 模型调用、精美动画、完整运营后台和大规模可观测性平台。

## Capabilities

### New Capabilities

- `auction-lifecycle`: 覆盖商家创建竞拍、配置竞拍规则、启动竞拍、状态流转、取消异常竞拍和结束竞拍。
- `realtime-bidding`: 覆盖用户进入竞拍房间、实时出价、后端规则校验、Redis 原子更新、排名计算和 WebSocket 房间广播。
- `order-settlement`: 覆盖竞拍达到成交条件后的最小订单生成和成交结果查询。

### Modified Capabilities

- 无。

## Impact

- 后端新增 Go 服务，提供 RESTful API、WebSocket 网关、竞拍领域服务、Redis 访问层和数据库访问层。
- 前端新增 React + TypeScript 的最小管理端和用户端页面，用于创建竞拍、进入房间、实时出价和查看成交结果。
- 数据库新增商品、竞拍、出价流水和订单相关表，数据库可使用 MySQL 或 PostgreSQL。
- Redis 新增竞拍热点状态、排行榜、幂等请求记录和房间广播辅助数据。
- 本地开发需要 Go、Node.js、Redis 和 MySQL/PostgreSQL 运行环境。
