export type AuctionStatus = "DRAFT" | "SCHEDULED" | "RUNNING" | "EXTENDED" | "SOLD" | "ENDED" | "CANCELLED";

// Auction 对应后端竞拍聚合的前端展示模型。
export type Auction = {
  id: string;
  merchantId: string;
  product: Product;
  rules: Rules;
  status: AuctionStatus;
  currentPrice: number;
  highestBidder?: string;
  endsAt?: string;
};

// Product 保存直播间展示所需的商品信息。
export type Product = {
  name: string;
  imageUrl: string;
  description: string;
};

// Rules 保存竞拍规则；时长字段由后端以纳秒 duration 序列化时仅用于展示。
export type Rules = {
  startPrice: number;
  increment: number;
  duration: number;
  ceilingPrice: number;
  extendThreshold: number;
  extendBy: number;
};

// Snapshot 是用户入房和 WebSocket 推送的实时竞拍状态。
export type Snapshot = {
  auctionId: string;
  product: Product;
  rules: Rules;
  status: AuctionStatus;
  currentPrice: number;
  highestBidder?: string;
  endsAt: string;
  serverTime: string;
  rank?: number;
  participants: number;
  nextMinimumBid: number;
};

// BidResult 描述一次出价处理结果。
export type BidResult = {
  bidId: string;
  snapshot: Snapshot;
  nextMinimum: number;
  extended: boolean;
  idempotent: boolean;
};

// API_BASE 支持通过 VITE_API_BASE 指向本地或远程后端。
const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8080";
const WS_BASE = API_BASE.replace(/^http/, "ws");

// listAuctions 获取商家管理端的竞拍列表。
export async function listAuctions(): Promise<Auction[]> {
  return request("/api/auctions");
}

// createAuction 创建 DRAFT 竞拍。
export async function createAuction(payload: {
  merchantId: string;
  productName: string;
  imageUrl: string;
  description: string;
  startPrice: number;
  increment: number;
  durationSeconds: number;
  ceilingPrice: number;
  extendThresholdSeconds: number;
  extendBySeconds: number;
}): Promise<Auction> {
  return request("/api/auctions", { method: "POST", body: JSON.stringify(payload) });
}

// startAuction 启动竞拍并返回实时快照。
export async function startAuction(id: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/start`, { method: "POST" });
}

// cancelAuction 取消未结束竞拍。
export async function cancelAuction(id: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/cancel`, { method: "POST" });
}

// getSnapshot 用于用户入房或断线重连后恢复状态。
export async function getSnapshot(id: string, userId: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/snapshot?userId=${encodeURIComponent(userId)}`);
}

// placeBid 提交带 requestId 的幂等出价。
export async function placeBid(id: string, userId: string, amount: number): Promise<BidResult> {
  return request(`/api/auctions/${id}/bids`, {
    method: "POST",
    body: JSON.stringify({ userId, requestId: crypto.randomUUID(), amount })
  });
}

// finishAuction 用于演示自然结束结算。
export async function finishAuction(id: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/finish`, { method: "POST" });
}

// getResult 查询竞拍最终结果和订单摘要。
export async function getResult(id: string): Promise<Record<string, unknown>> {
  return request(`/api/auctions/${id}/result`);
}

// openAuctionSocket 订阅指定竞拍房间的实时事件。
export function openAuctionSocket(id: string, userId: string): WebSocket {
  return new WebSocket(`${WS_BASE}/ws/auctions/${id}?userId=${encodeURIComponent(userId)}`);
}

// request 统一处理 JSON 请求和错误响应。
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) }
  });
  const data = await res.json();
  if (!res.ok) {
    throw new Error(data.error ?? "请求失败");
  }
  return data;
}
