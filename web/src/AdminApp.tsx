import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  adminCancelAuction,
  adminCreateAuction,
  adminListAuctions,
  adminStartAuction,
  APIError,
  openAuctionSocket,
  type Auction,
  type AuctionEvent,
  type CreateAuctionPayload,
  type LeaderboardEntry,
  type LoginSession,
  type Snapshot
} from "./api";

type AdminAppProps = {
  session: LoginSession;
  onLogout: () => void;
};

type LogItem = {
  id: string;
  text: string;
  time: string;
};

const defaultForm: CreateAuctionPayload = {
  merchantId: "merchant-demo",
  productName: "星河翡翠手镯",
  imageUrl: "https://images.unsplash.com/photo-1617038260897-41a1f14a8ca0?auto=format&fit=crop&w=1200&q=80",
  description: "直播间限时竞拍样品，支持封顶成交、自动延时和到点结算。",
  startPrice: 0,
  increment: 100,
  durationSeconds: 180,
  ceilingPrice: 3000,
  extendThresholdSeconds: 20,
  extendBySeconds: 30
};

export function AdminApp({ session, onLogout }: AdminAppProps) {
  const [form, setForm] = useState<CreateAuctionPayload>(defaultForm);
  const [auctions, setAuctions] = useState<Auction[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [logs, setLogs] = useState<LogItem[]>([]);
  const [connection, setConnection] = useState("未连接");
  const [now, setNow] = useState(() => Date.now());
  const socketRef = useRef<WebSocket | null>(null);
  const selected = useMemo(() => auctions.find((item) => item.id === selectedId), [auctions, selectedId]);
  const live = snapshot?.auctionId === selectedId ? snapshot : null;
  const currentStatus = live?.status ?? selected?.status;
  const currentPrice = live?.currentPrice ?? selected?.currentPrice ?? 0;
  const currentEndsAt = live?.endsAt ?? selected?.endsAt;

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!selectedId) return;
    socketRef.current?.close();
    setConnection("连接中");
    const socket = openAuctionSocket(selectedId, session.token);
    socketRef.current = socket;
    socket.onopen = () => setConnection("实时同步");
    socket.onerror = () => setConnection("连接异常");
    socket.onclose = () => {
      if (socketRef.current === socket) setConnection("已断开");
    };
    socket.onmessage = (event) => handleRealtimeEvent(JSON.parse(event.data) as AuctionEvent);
    return () => socket.close();
  }, [selectedId, session.token]);

  async function refresh(preferredId = selectedId) {
    try {
      const items = await adminListAuctions(session.token);
      setAuctions(items);
      setSelectedId(preferredId || items[0]?.id || "");
    } catch (err) {
      if (err instanceof APIError && err.code === "UNAUTHORIZED") return onLogout();
      setMessage(err instanceof Error ? err.message : "列表读取失败");
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await run("竞拍已创建", async () => {
      const created = await adminCreateAuction(session.token, form);
      appendLog(`创建了竞拍：${created.product.name}`);
      await refresh(created.id);
    });
  }

  async function startAuction() {
    if (!selected) return;
    await run("竞拍已启动", async () => {
      const next = await adminStartAuction(session.token, selected.id);
      applySnapshot(next);
      appendLog(`${session.user.displayName} 开始了竞拍`);
    });
  }

  async function cancelAuction() {
    if (!selected) return;
    await run("竞拍已取消", async () => {
      const next = await adminCancelAuction(session.token, selected.id);
      applySnapshot(next);
      appendLog(`${session.user.displayName} 取消了竞拍`);
    });
  }

  async function run(success: string, action: () => Promise<void>) {
    setLoading(true);
    setMessage("");
    try {
      await action();
      setMessage(success);
    } catch (err) {
      if (err instanceof APIError && err.code === "UNAUTHORIZED") return onLogout();
      setMessage(err instanceof Error ? err.message : "操作失败");
    } finally {
      setLoading(false);
    }
  }

  function handleRealtimeEvent(event: AuctionEvent) {
    if (event.snapshot) applySnapshot(event.snapshot);
    const text = eventText(event);
    if (text) appendLog(text);
  }

  function applySnapshot(next: Snapshot) {
    setSnapshot(next);
    setAuctions((items) => items.map((item) => (item.id === next.auctionId ? snapshotToAuction(item, next) : item)));
  }

  function appendLog(text: string) {
    setLogs((items) => [{ id: crypto.randomUUID(), text, time: formatTime(new Date().toISOString()) }, ...items].slice(0, 12));
  }

  return (
    <main className="admin-shell">
      <aside className="admin-sidebar">
        <div className="brand-block">
          <p className="eyebrow">Admin Console</p>
          <h1>竞拍管理</h1>
          <span>{session.user.displayName}</span>
        </div>
        <button className="ghost-btn" onClick={onLogout}>退出</button>
        <div className="auction-stack">
          {auctions.map((item) => (
            <button key={item.id} className={item.id === selectedId ? "auction-item active" : "auction-item"} onClick={() => setSelectedId(item.id)}>
              <span>{item.product.name}</span>
              <StatusPill status={item.status} />
            </button>
          ))}
        </div>
      </aside>

      <section className="admin-main">
        <header className="admin-header">
          <div>
            <p className="eyebrow">Current Lot · {connection}</p>
            <h2>{selected?.product.name || "新建竞拍"}</h2>
          </div>
          {selected && <strong>{currency(currentPrice)}</strong>}
        </header>

        <div className="admin-grid">
          <form className="auction-form" onSubmit={submit}>
            <h3>发布竞拍</h3>
            <label>商品名称<input value={form.productName} onChange={(event) => setForm({ ...form, productName: event.target.value })} /></label>
            <label>商品图片<input value={form.imageUrl} onChange={(event) => setForm({ ...form, imageUrl: event.target.value })} /></label>
            <label className="wide">商品介绍<textarea value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></label>
            <NumberField label="起拍价" value={form.startPrice} onChange={(value) => setForm({ ...form, startPrice: value })} />
            <NumberField label="加价幅度" value={form.increment} onChange={(value) => setForm({ ...form, increment: value })} />
            <NumberField label="竞拍时长(秒)" value={form.durationSeconds} onChange={(value) => setForm({ ...form, durationSeconds: value })} />
            <NumberField label="封顶价" value={form.ceilingPrice} onChange={(value) => setForm({ ...form, ceilingPrice: value })} />
            <NumberField label="延时窗口(秒)" value={form.extendThresholdSeconds} onChange={(value) => setForm({ ...form, extendThresholdSeconds: value })} />
            <NumberField label="延长时长(秒)" value={form.extendBySeconds} onChange={(value) => setForm({ ...form, extendBySeconds: value })} />
            <button className="primary-btn" disabled={loading}>创建竞拍</button>
          </form>

          <section className="lot-panel">
            {selected ? (
              <>
                <div className="lot-image" style={{ backgroundImage: `url(${selected.product.imageUrl})` }} />
                <div className="lot-summary">
                  <StatusPill status={currentStatus ?? selected.status} />
                  <p>{selected.product.description}</p>
                  <dl>
                    <div><dt>当前最高价</dt><dd>{currency(currentPrice)}</dd></div>
                    <div><dt>最高出价人</dt><dd>{live?.leaderboard?.[0]?.displayName || selected.highestBidder || "-"}</dd></div>
                    <div><dt>倒计时</dt><dd>{currentEndsAt ? countdown(currentEndsAt, now) : "-"}</dd></div>
                    <div><dt>参与人数</dt><dd>{live?.participants ?? "-"}</dd></div>
                  </dl>
                </div>
                <div className="admin-actions">
                  <button onClick={startAuction} disabled={loading || currentStatus !== "DRAFT"}>启动</button>
                  <button className="danger-btn" onClick={cancelAuction} disabled={loading || !["DRAFT", "RUNNING"].includes(currentStatus ?? "")}>取消</button>
                </div>
              </>
            ) : (
              <div className="empty-state">暂无竞拍</div>
            )}
            {message && <p className="notice-line">{message}</p>}
            <Leaderboard items={live?.leaderboard ?? []} />
            <EventLog title="实时日志" logs={logs} />
          </section>
        </div>
      </section>
    </main>
  );
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label>{label}<input type="number" value={value} onChange={(event) => onChange(Number(event.target.value))} /></label>;
}

function Leaderboard({ items }: { items: LeaderboardEntry[] }) {
  return (
    <section className="leaderboard">
      <h3>Top 5 排行榜</h3>
      {items.length === 0 ? <p className="empty-log">暂无出价</p> : items.map((item) => (
        <p key={item.userId}>
          <span>#{item.rank}</span>
          <strong>{item.displayName || item.userId}</strong>
          <em>{currency(item.amount)}</em>
        </p>
      ))}
    </section>
  );
}

function EventLog({ title, logs }: { title: string; logs: LogItem[] }) {
  return (
    <section className="event-log">
      <h3>{title}</h3>
      {logs.length === 0 ? <p className="empty-log">等待实时事件</p> : logs.map((item) => <p key={item.id}><span>{item.time}</span>{item.text}</p>)}
    </section>
  );
}

function StatusPill({ status }: { status: Auction["status"] }) {
  return <em className={`status-pill ${status.toLowerCase()}`}>{status}</em>;
}

function snapshotToAuction(item: Auction, next: Snapshot): Auction {
  return { ...item, status: next.status, currentPrice: next.currentPrice, highestBidder: next.highestBidder, endsAt: next.endsAt, updatedAt: next.serverTime };
}

function eventText(event: AuctionEvent) {
  const price = event.snapshot ? currency(event.snapshot.currentPrice) : "";
  if (event.type === "snapshot" && event.meta?.actorName) return `${event.meta.actorName} 开始了竞拍`;
  if (event.type === "bidAccepted") return `${event.meta?.bidderName || event.meta?.bidderId || "用户"} 出价 ${price}，当前最高价 ${price}`;
  if (event.type === "auctionExtended") return `${event.meta?.bidderName || "用户"} 出价触发延时，当前最高价 ${price}`;
  if (event.type === "auctionCancelled") return `${event.meta?.actorName || "管理员"} 取消了竞拍`;
  if (event.type === "auctionEnded") return event.reason === "ceiling_price_reached" ? `达到封顶价，竞拍结束：${price}` : "时间到，竞拍结束";
  return "";
}

function currency(value: number) {
  return `¥${value.toLocaleString("zh-CN")}`;
}

function countdown(endsAt: string, now: number) {
  const diff = Math.max(0, new Date(endsAt).getTime() - now);
  const totalSeconds = Math.ceil(diff / 1000);
  const minutes = Math.floor(totalSeconds / 60).toString().padStart(2, "0");
  const seconds = (totalSeconds % 60).toString().padStart(2, "0");
  return `${minutes}:${seconds}`;
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}
