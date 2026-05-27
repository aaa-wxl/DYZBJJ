export type AuctionStatus = "DRAFT" | "RUNNING" | "SOLD" | "ENDED" | "CANCELLED";
export type Role = "admin" | "bidder";

export type User = {
  id: string;
  name: string;
  role: Role;
  createdAt: string;
};

export type LoginSession = {
  token: string;
  user: User;
};

export type Auction = {
  id: string;
  merchantId: string;
  product: Product;
  rules: Rules;
  status: AuctionStatus;
  currentPrice: number;
  highestBidder?: string;
  startsAt?: string;
  endsAt?: string;
  soldAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type Product = {
  name: string;
  imageUrl: string;
  description: string;
};

export type Rules = {
  startPrice: number;
  increment: number;
  duration: number;
  ceilingPrice: number;
  extendThreshold: number;
  extendBy: number;
};

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

export type BidResult = {
  bidId: string;
  snapshot: Snapshot;
  nextMinimum: number;
  extended: boolean;
  idempotent: boolean;
};

export type AuctionEvent = {
  type: "snapshot" | "bidAccepted" | "auctionExtended" | "auctionEnded" | "auctionCancelled";
  auctionId?: string;
  snapshot?: Snapshot;
  reason?: string;
  serverTime?: string;
  meta?: Record<string, string>;
};

export type AuctionResult = {
  auction: Auction;
  order?: {
    id: string;
    auctionId: string;
    productName: string;
    buyerId: string;
    finalPrice: number;
    status: string;
    createdAt: string;
  };
};

export type CreateAuctionPayload = {
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
};

type APIErrorBody = {
  code?: string;
  message?: string;
  details?: unknown;
};

export class APIError extends Error {
  code: string;
  details?: unknown;

  constructor(body: APIErrorBody) {
    super(body.message || "请求失败");
    this.name = "APIError";
    this.code = body.code || "REQUEST_FAILED";
    this.details = body.details;
  }
}

const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8080";
const WS_BASE = API_BASE.replace(/^http/, "ws");

export async function login(name: string, role: Role): Promise<LoginSession> {
  return request("/api/login", undefined, {
    method: "POST",
    body: JSON.stringify({ name, role })
  });
}

export async function adminListAuctions(token: string): Promise<Auction[]> {
  return request("/api/admin/auctions", token);
}

export async function adminCreateAuction(token: string, payload: CreateAuctionPayload): Promise<Auction> {
  return request("/api/admin/auctions", token, {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export async function adminStartAuction(token: string, id: string): Promise<Snapshot> {
  return request(`/api/admin/auctions/${id}/start`, token, { method: "POST" });
}

export async function adminCancelAuction(token: string, id: string): Promise<Snapshot> {
  return request(`/api/admin/auctions/${id}/cancel`, token, { method: "POST" });
}

export async function listAuctions(token: string): Promise<Auction[]> {
  return request("/api/auctions", token);
}

export async function getSnapshot(token: string, id: string): Promise<Snapshot> {
  return request(`/api/auctions/${id}/snapshot`, token);
}

export async function placeBid(token: string, id: string, amount: number): Promise<BidResult> {
  return request(`/api/auctions/${id}/bids`, token, {
    method: "POST",
    body: JSON.stringify({ requestId: crypto.randomUUID(), amount })
  });
}

export async function getResult(token: string, id: string): Promise<AuctionResult> {
  return request(`/api/auctions/${id}/result`, token);
}

export function openAuctionSocket(id: string, token: string): WebSocket {
  return new WebSocket(`${WS_BASE}/ws/auctions/${id}?token=${encodeURIComponent(token)}`);
}

async function request<T>(path: string, token?: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init.headers as Record<string, string> | undefined)
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  const data = await readJSON(res);
  if (!res.ok) {
    throw new APIError(data as APIErrorBody);
  }
  return data as T;
}

async function readJSON(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch {
    return { message: text };
  }
}
