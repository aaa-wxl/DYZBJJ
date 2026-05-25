## ADDED Requirements

### Requirement: 用户可以进入竞拍房间
系统 MUST 允许用户按 `auctionId` 进入竞拍房间，并获取当前竞拍快照。

#### Scenario: 进入有效竞拍房间
- **WHEN** 用户请求进入存在的竞拍房间
- **THEN** 系统 MUST 返回竞拍商品信息、规则、当前价、当前最高出价人、结束时间、竞拍状态和用户当前排名

#### Scenario: 拒绝进入不存在的竞拍房间
- **WHEN** 用户请求进入不存在的 `auctionId`
- **THEN** 系统 MUST 返回不存在错误，且不得创建新的竞拍状态

### Requirement: 系统校验实时出价
系统 MUST 在服务端校验每一笔出价，确保出价符合竞拍状态、起拍价、加价幅度和封顶价规则。

#### Scenario: 接受有效出价
- **WHEN** 用户在 `RUNNING` 或 `EXTENDED` 状态竞拍中提交高于当前价且符合加价幅度的出价
- **THEN** 系统 MUST 接受出价，更新当前价、最高出价人、出价流水和排行榜

#### Scenario: 拒绝低价出价
- **WHEN** 用户提交低于最低有效出价的金额
- **THEN** 系统 MUST 拒绝出价，并返回当前价和下一次最低有效出价

#### Scenario: 拒绝非运行中竞拍出价
- **WHEN** 用户对 `DRAFT`、`SCHEDULED`、`SOLD`、`ENDED` 或 `CANCELLED` 状态的竞拍提交出价
- **THEN** 系统 MUST 拒绝出价，并返回当前竞拍状态

### Requirement: 出价更新必须原子化
系统 MUST 使用 Redis 原子操作处理竞拍热点状态，保证并发出价下只有符合规则的最高出价生效。

#### Scenario: 并发出价只接受有效最高序列
- **WHEN** 多个用户几乎同时对同一竞拍提交出价
- **THEN** 系统 MUST 按原子校验结果更新竞拍状态，避免当前价回退、重复成交或排行榜错乱

#### Scenario: 重复请求保持幂等
- **WHEN** 同一用户使用相同 `requestId` 重复提交同一笔出价
- **THEN** 系统 MUST 不重复写入出价流水，并返回一致的处理结果

### Requirement: 系统按房间广播竞拍事件
系统 MUST 通过 WebSocket 按 `auctionId` 房间广播竞拍事件，确保不同竞拍房间互不干扰。

#### Scenario: 广播出价成功事件
- **WHEN** 一笔出价被系统接受
- **THEN** 系统 MUST 向对应 `auctionId` 房间广播当前价、最高出价人、排行榜变化、服务端时间和结束时间

#### Scenario: 广播自动延时事件
- **WHEN** 有效出价触发自动延时
- **THEN** 系统 MUST 向对应 `auctionId` 房间广播新的结束时间和延时原因

#### Scenario: 广播竞拍结束事件
- **WHEN** 竞拍状态变更为 `SOLD`、`ENDED` 或 `CANCELLED`
- **THEN** 系统 MUST 向对应 `auctionId` 房间广播最终状态和结果摘要

### Requirement: 客户端可以重连并恢复状态
系统 MUST 支持用户 WebSocket 断开后重新进入房间并恢复最新竞拍状态。

#### Scenario: 重连后获取最新快照
- **WHEN** 用户 WebSocket 断开后重新连接同一 `auctionId` 房间
- **THEN** 系统 MUST 返回最新竞拍快照，包含当前价、排行榜、状态、服务端时间和结束时间
