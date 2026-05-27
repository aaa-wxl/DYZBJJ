import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import {
  APIError,
  getResult,
  getSnapshot,
  listAuctions,
  openAuctionSocket,
  placeBid,
  type Auction,
  type LoginSession,
  type Snapshot
} from "./api";

type MobileAppProps = {
  session: LoginSession;
  onLogout: () => void;
};

export function MobileApp({ session, onLogout }: MobileAppProps) {
  const [auctions, setAuctions] = useState<Auction[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [bidAmount, setBidAmount] = useState(0);
  const [connection, setConnection] = useState("连接中");
  const [message, setMessage] = useState("");
  const reconnectTimer = useRef<number | null>(null);
  const selected = useMemo(() => auctions.find((item) => item.id === selectedId), [auctions, selectedId]);
  const display = snapshot || selected;

  useEffect(() => {
    void loadList();
    return () => {
      if (reconnectTimer.current) window.clearTimeout(reconnectTimer.current);
    };
  }, []);

  useEffect(() => {
    if (!selectedId) return;
    let closed = false;

    async function connect() {
      setConnection("连接中");
      await loadSnapshot(selectedId);
      const socket = openAuctionSocket(selectedId, session.token);
      socket.onopen = () => setConnection("实时连接");
      socket.onmessage = (event) => {
        const payload = JSON.parse(event.data);
        const next = payload.snapshot ?? payload.Snapshot;
        if (next) {
          applySnapshot(next);
        }
      };
      socket.onerror = () => setConnection("连接异常");
      socket.onclose = () => {
        if (closed) return;
        setConnection("正在恢复");
        reconnectTimer.current = window.setTimeout(() => void connect(), 1200);
      };
      return socket;
    }

    let socket: WebSocket | undefined;
    void connect().then((next) => {
      socket = next;
    });

    return () => {
      closed = true;
      socket?.close();
      if (reconnectTimer.current) window.clearTimeout(reconnectTimer.current);
    };
  }, [selectedId, session.token]);

  async function loadList() {
    try {
      const items = await listAuctions(session.token);
      setAuctions(items);
      const running = items.find((item) => item.status === "RUNNING");
      setSelectedId((current) => current || running?.id || items[0]?.id || "");
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "列表读取失败");
    }
  }

  async function loadSnapshot(id: string) {
    try {
      applySnapshot(await getSnapshot(session.token, id));
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "状态恢复失败");
    }
  }

  function applySnapshot(next: Snapshot) {
    setSnapshot(next);
    setBidAmount(next.nextMinimumBid);
    if (next.status === "SOLD") {
      setMessage("已成交");
    } else if (next.status === "ENDED") {
      setMessage("已结束");
    }
  }

  async function submitBid(event: FormEvent) {
    event.preventDefault();
    if (!selectedId) return;
    setMessage("");
    try {
      const result = await placeBid(session.token, selectedId, bidAmount);
      applySnapshot(result.snapshot);
      if (result.snapshot.status === "SOLD") {
        const finalResult = await getResult(session.token, selectedId);
        setMessage(finalResult.order ? `成交价 ${currency(finalResult.order.finalPrice)}` : "已成交");
      } else {
        setMessage("出价成功");
      }
    } catch (err) {
      if (err instanceof APIError && err.details && typeof err.details === "object" && "nextMinimumBid" in err.details) {
        const next = Number((err.details as { nextMinimumBid: number }).nextMinimumBid);
        if (Number.isFinite(next)) setBidAmount(next);
      }
      setMessage(err instanceof Error ? err.message : "出价失败");
    }
  }

  return (
    <main className="mobile-shell">
      <header className="mobile-topbar">
        <div>
          <p>{session.user.name}</p>
          <strong>{connection}</strong>
        </div>
        <button className="text-btn" onClick={onLogout}>退出</button>
      </header>

      <div className="lot-tabs">
        {auctions.map((item) => (
          <button key={item.id} className={item.id === selectedId ? "active" : ""} onClick={() => setSelectedId(item.id)}>
            {item.product.name}
          </button>
        ))}
      </div>

      {display ? (
        <>
          <section className="product-hero">
            <img src={display.product.imageUrl} alt={display.product.name} />
            <div className="hero-status">
              <span>{display.status}</span>
              <strong>{currency(display.currentPrice)}</strong>
            </div>
          </section>

          <section className="product-copy">
            <h1>{display.product.name}</h1>
            <p>{display.product.description}</p>
          </section>

          <section className="mobile-metrics">
            <Metric label="倒计时" value={snapshot ? countdown(snapshot.endsAt, snapshot.serverTime) : "-"} />
            <Metric label="最低出价" value={currency(snapshot?.nextMinimumBid ?? display.rules.startPrice)} />
            <Metric label="我的排名" value={snapshot?.rank ? `第 ${snapshot.rank}` : "-"} />
            <Metric label="参与人数" value={`${snapshot?.participants ?? 0}`} />
          </section>

          {message && <p className="mobile-message">{message}</p>}

          <form className="bid-dock" onSubmit={submitBid}>
            <div>
              <span>当前价</span>
              <strong>{currency(snapshot?.currentPrice ?? display.currentPrice)}</strong>
            </div>
            <input type="number" value={bidAmount} onChange={(event) => setBidAmount(Number(event.target.value))} />
            <button disabled={!snapshot || snapshot.status !== "RUNNING"}>出价</button>
          </form>
        </>
      ) : (
        <section className="mobile-empty">暂无可参与竞拍</section>
      )}
    </main>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function currency(value: number) {
  return `￥${value.toLocaleString("zh-CN")}`;
}

function countdown(endsAt: string, serverTime: string) {
  const diff = Math.max(0, new Date(endsAt).getTime() - new Date(serverTime).getTime());
  const totalSeconds = Math.ceil(diff / 1000);
  const minutes = Math.floor(totalSeconds / 60).toString().padStart(2, "0");
  const seconds = (totalSeconds % 60).toString().padStart(2, "0");
  return `${minutes}:${seconds}`;
}
