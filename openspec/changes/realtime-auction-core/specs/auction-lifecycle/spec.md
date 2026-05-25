## ADDED Requirements

### Requirement: 商家可以创建竞拍
系统 MUST 允许商家创建竞拍商品，并配置竞拍规则，包括商品名称、商品图片、商品介绍、起拍价、加价幅度、竞拍时长、封顶价和自动延时规则。

#### Scenario: 创建有效竞拍
- **WHEN** 商家提交完整且合法的商品信息和竞拍规则
- **THEN** 系统创建状态为 `DRAFT` 的竞拍记录，并返回竞拍详情

#### Scenario: 拒绝非法竞拍规则
- **WHEN** 商家提交缺失必填项、加价幅度小于等于 0、竞拍时长小于等于 0 或封顶价低于起拍价的规则
- **THEN** 系统 MUST 拒绝创建竞拍，并返回明确的校验错误

### Requirement: 商家可以启动竞拍
系统 MUST 允许商家启动处于 `DRAFT` 或 `SCHEDULED` 状态的竞拍，并初始化实时竞拍状态。

#### Scenario: 启动竞拍成功
- **WHEN** 商家启动一个规则完整且未开始的竞拍
- **THEN** 系统 MUST 将竞拍状态变更为 `RUNNING`，初始化当前价、结束时间和 Redis 热点状态

#### Scenario: 拒绝重复启动
- **WHEN** 商家尝试启动已经处于 `RUNNING`、`EXTENDED`、`SOLD`、`ENDED` 或 `CANCELLED` 状态的竞拍
- **THEN** 系统 MUST 拒绝启动请求，并保持原状态不变

### Requirement: 系统维护竞拍状态机
系统 MUST 通过统一状态机管理竞拍状态，禁止绕过状态机直接修改竞拍状态。

#### Scenario: 自动延时触发
- **WHEN** `RUNNING` 状态竞拍在结束前的延时窗口内收到有效出价，且未达到封顶价
- **THEN** 系统 MUST 将竞拍结束时间延后，并将状态保持为 `RUNNING` 或变更为 `EXTENDED`

#### Scenario: 封顶价触发成交
- **WHEN** 有效出价达到或超过竞拍封顶价
- **THEN** 系统 MUST 将竞拍状态变更为 `SOLD`，记录成交用户和成交价格，并禁止后续出价

#### Scenario: 自然结束竞拍
- **WHEN** 当前时间超过竞拍结束时间，且竞拍未达到封顶价
- **THEN** 系统 MUST 根据是否存在有效出价将竞拍变更为 `SOLD` 或 `ENDED`

### Requirement: 商家可以取消异常竞拍
系统 MUST 允许商家取消尚未结束的异常竞拍。

#### Scenario: 取消运行中竞拍
- **WHEN** 商家取消处于 `DRAFT`、`SCHEDULED`、`RUNNING` 或 `EXTENDED` 状态的竞拍
- **THEN** 系统 MUST 将竞拍状态变更为 `CANCELLED`，并广播竞拍取消事件

#### Scenario: 拒绝取消已结束竞拍
- **WHEN** 商家尝试取消处于 `SOLD`、`ENDED` 或 `CANCELLED` 状态的竞拍
- **THEN** 系统 MUST 拒绝取消请求，并保持竞拍结果不变
