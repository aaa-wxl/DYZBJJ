import { useEffect, useMemo, useState } from "react";
import {
  Auction,
  Snapshot,
  cancelAuction,
  createAuction,
  finishAuction,
  getResult,
  listAuctions,
  openAuctionSocket,
  placeBid,
  startAuction
} from "./api";

type LogItem = {
  id: string;
  message: string;
};

const defaultAuction = {
  merchantId: "merchant-demo",
  productName: "星河翡翠手镯",
  imageUrl: "https://images.unsplash.com/photo-1617038260897-41a1f14a8ca0?auto=format&fit=crop&w=900&q=80",
  description: "直播间限时竞拍样品，适合演示封顶成交和自动延时。",
  startPrice: 0,
  increment: 100,
  durationSeconds: 180,
  ceilingPrice: 3000,
  extendThresholdSeconds: 20,
  extendBySeconds: 30
};

// App 组合主播端管理、用户端出价和实时事件日志，形成演示闭环。
export function App() {
  const [form, setForm] = useState(defaultAuction);
  const [auctions, setAuctions] = useState<Auction[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [userId, setUserId] = useState("user-demo");
  const [bidAmount, setBidAmount] = useState(100);
  const [logs, setLogs] = useState<LogItem[]>([]);
  const [result, setResult] = useState<string>("");

  const selected = useMemo(() => auctions.find((item) => item.id === selectedId), [auctions, selectedId]);

  // 首次进入页面时加载已有竞拍，便于接续演示。
  useEffect(() => {
    void refreshAuctions();
  }, []);

  // 选择竞拍或用户变化时重建 WebSocket 订阅，用最新快照恢复页面。
  useEffect(() => {
    if (!selectedId) {
      return;
    }
    const socket = openAuctionSocket(selectedId, userId);
    socket.onmessage = (event) => {
      const payload = JSON.parse(event.data);
      const nextSnapshot = payload.snapshot ?? payload.Snapshot;
      if (nextSnapshot) {
        setSnapshot(nextSnapshot);
        setBidAmount(nextSnapshot.nextMinimumBid);
        appendLog(`${payload.type ?? payload.Type ?? "snapshot"}：当前价 ${nextSnapshot.currentPrice}`);
      }
    };
    socket.onopen = () => appendLog("WebSocket 已连接");
    socket.onclose = () => appendLog("WebSocket 已断开");
    socket.onerror = () => appendLog("WebSocket 连接异常");
    return () => socket.close();
  }, [selectedId, userId]);

  // refreshAuctions 同步商家端列表，并默认选中第一场竞拍。
  async function refreshAuctions() {
    const items = await listAuctions();
    setAuctions(items);
    if (!selectedId && items.length > 0) {
      setSelectedId(items[0].id);
    }
  }

  // appendLog 保留最近事件，避免实时日志无限增长。
  function appendLog(message: string) {
    setLogs((items) => [{ id: crypto.randomUUID(), message }, ...items].slice(0, 8));
  }

  // handleCreate 创建竞拍后立即选中新记录。
  async function handleCreate() {
    const created = await createAuction(form);
    appendLog(`已创建竞拍 ${created.id}`);
    await refreshAuctions();
    setSelectedId(created.id);
  }

  // handleStart 触发后端初始化实时竞拍状态。
  async function handleStart() {
    if (!selectedId) return;
    const next = await startAuction(selectedId);
    setSnapshot(next);
    setBidAmount(next.nextMinimumBid);
    appendLog("竞拍已启动");
    await refreshAuctions();
  }

  // handleCancel 演示主播异常取消竞拍。
  async function handleCancel() {
    if (!selectedId) return;
    const next = await cancelAuction(selectedId);
    setSnapshot(next);
    appendLog("竞拍已取消");
    await refreshAuctions();
  }

  // handleFinish 演示自然结束或手动结算到期竞拍。
  async function handleFinish() {
    if (!selectedId) return;
    const next = await finishAuction(selectedId);
    setSnapshot(next);
    appendLog(`竞拍结束：${next.status}`);
    await refreshAuctions();
  }

  // handleBid 提交用户出价，并将下一口价反馈到输入框。
  async function handleBid() {
    if (!selectedId) return;
    try {
      const bid = await placeBid(selectedId, userId, bidAmount);
      setSnapshot(bid.snapshot);
      setBidAmount(bid.snapshot.nextMinimumBid);
      appendLog(`出价成功：${bid.snapshot.currentPrice}`);
    } catch (error) {
      appendLog(error instanceof Error ? error.message : "出价失败");
    }
  }

  // handleResult 展示后端返回的最终竞拍和订单摘要。
  async function handleResult() {
    if (!selectedId) return;
    const data = await getResult(selectedId);
    setResult(JSON.stringify(data, null, 2));
  }

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Realtime Auction Core</p>
          <h1>实时竞拍控制台</h1>
        </div>
        <div className="status-strip">
          <span>{snapshot?.status ?? selected?.status ?? "未选择"}</span>
          <strong>{currency(snapshot?.currentPrice ?? selected?.currentPrice ?? 0)}</strong>
        </div>
      </section>

      <section className="workspace">
        <div className="panel manager">
          <div className="panel-head">
            <h2>主播端</h2>
            <button onClick={handleCreate}>创建竞拍</button>
          </div>
          <div className="form-grid">
            <label>
              商品名称
              <input value={form.productName} onChange={(e) => setForm({ ...form, productName: e.target.value })} />
            </label>
            <label>
              商品图片
              <input value={form.imageUrl} onChange={(e) => setForm({ ...form, imageUrl: e.target.value })} />
            </label>
            <label className="wide">
              商品介绍
              <textarea value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </label>
            <NumberField label="起拍价" value={form.startPrice} onChange={(value) => setForm({ ...form, startPrice: value })} />
            <NumberField label="加价幅度" value={form.increment} onChange={(value) => setForm({ ...form, increment: value })} />
            <NumberField label="竞拍时长(秒)" value={form.durationSeconds} onChange={(value) => setForm({ ...form, durationSeconds: value })} />
            <NumberField label="封顶价" value={form.ceilingPrice} onChange={(value) => setForm({ ...form, ceilingPrice: value })} />
            <NumberField label="延时窗口(秒)" value={form.extendThresholdSeconds} onChange={(value) => setForm({ ...form, extendThresholdSeconds: value })} />
            <NumberField label="延长时长(秒)" value={form.extendBySeconds} onChange={(value) => setForm({ ...form, extendBySeconds: value })} />
          </div>

          <div className="auction-list">
            {auctions.map((item) => (
              <button className={item.id === selectedId ? "auction-row active" : "auction-row"} key={item.id} onClick={() => setSelectedId(item.id)}>
                <span>{item.product.name}</span>
                <em>{item.status}</em>
              </button>
            ))}
          </div>

          <div className="actions">
            <button onClick={handleStart} disabled={!selectedId}>启动</button>
            <button onClick={handleCancel} disabled={!selectedId}>取消</button>
            <button onClick={handleFinish} disabled={!selectedId}>结算结束</button>
            <button onClick={handleResult} disabled={!selectedId}>查看结果</button>
          </div>
        </div>

        <div className="panel live">
          <div className="video-stage" style={{ backgroundImage: `linear-gradient(90deg, rgba(10,15,20,.82), rgba(10,15,20,.25)), url(${selected?.product.imageUrl ?? form.imageUrl})` }}>
            <div>
              <p>直播间竞拍</p>
              <h2>{selected?.product.name ?? form.productName}</h2>
            </div>
            <div className="price-tag">{currency(snapshot?.currentPrice ?? 0)}</div>
          </div>

          <div className="bid-grid">
            <Metric label="状态" value={snapshot?.status ?? selected?.status ?? "-"} />
            <Metric label="参与人数" value={String(snapshot?.participants ?? 0)} />
            <Metric label="我的排名" value={snapshot?.rank ? `#${snapshot.rank}` : "-"} />
            <Metric label="最低出价" value={currency(snapshot?.nextMinimumBid ?? bidAmount)} />
          </div>

          <div className="bidline">
            <input value={userId} onChange={(e) => setUserId(e.target.value)} />
            <input type="number" value={bidAmount} onChange={(e) => setBidAmount(Number(e.target.value))} />
            <button onClick={handleBid} disabled={!selectedId}>立即出价</button>
          </div>

          <div className="timeline">
            {logs.map((item) => (
              <p key={item.id}>{item.message}</p>
            ))}
          </div>
        </div>
      </section>

      {result && <pre className="result">{result}</pre>}
    </main>
  );
}

// NumberField 统一管理竞拍规则中的数字输入项。
function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <label>
      {label}
      <input type="number" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}

// Metric 展示用户端实时竞拍指标。
function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

// currency 统一前端金额展示格式。
function currency(value: number) {
  return `¥${value.toLocaleString("zh-CN")}`;
}
