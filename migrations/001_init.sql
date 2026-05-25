-- 商品表保存竞拍商品的基础展示信息。
CREATE TABLE IF NOT EXISTS products (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    image_url TEXT,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 竞拍表保存规则、状态机状态和最终成交快照。
CREATE TABLE IF NOT EXISTS auctions (
    id VARCHAR(64) PRIMARY KEY,
    merchant_id VARCHAR(64) NOT NULL,
    product_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    start_price BIGINT NOT NULL,
    increment BIGINT NOT NULL,
    duration_seconds BIGINT NOT NULL,
    ceiling_price BIGINT NOT NULL,
    extend_threshold_seconds BIGINT NOT NULL,
    extend_by_seconds BIGINT NOT NULL,
    current_price BIGINT NOT NULL,
    highest_bidder VARCHAR(64),
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    sold_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 出价流水表保存已接受出价，auction_id + request_id 保证请求幂等。
CREATE TABLE IF NOT EXISTS bids (
    id VARCHAR(64) PRIMARY KEY,
    auction_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    request_id VARCHAR(128) NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_bids_request UNIQUE (auction_id, request_id)
);

-- 订单表保存最小成交记录，auction_id 唯一约束保证一场竞拍最多一个有效订单。
CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(64) PRIMARY KEY,
    auction_id VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    buyer_id VARCHAR(64) NOT NULL,
    final_price BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_orders_auction UNIQUE (auction_id)
);

-- 常用查询索引用于竞拍列表和排行榜审计。
CREATE INDEX IF NOT EXISTS idx_auctions_status ON auctions(status);
CREATE INDEX IF NOT EXISTS idx_bids_auction_amount ON bids(auction_id, amount DESC);
