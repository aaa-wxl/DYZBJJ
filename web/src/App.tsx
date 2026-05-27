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

type ActionKey = "create" | "start" | "cancel" | "finish" | "result" | "bid";

type ActionFeedback = {
  tone: "idle" | "pending" | "success" | "error";
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
  const [now, setNow] = useState(() => Date.now());
  const [pendingAction, setPendingAction] = useState<ActionKey | null>(null);
  const [lastAction, setLastAction] = useState<ActionKey | null>(null);
  const [feedback, setFeedback] = useState<ActionFeedback>({ tone: "idle", message: "等待操作" });

  const selected = useMemo(() => auctions.find((item) => item.id === selectedId), [auctions, selectedId]);
  const remainingLabel = useMemo(() => formatRemaining(snapshot?.endsAt, now), [snapshot?.endsAt, now]);

  // 首次进入页面时加载已有竞拍，便于接续演示。
  useEffect(() => {
    void refreshAuctions();
  }, []);

  // 每秒刷新一次倒计时，确保用户入房和重连后都能看到剩余时间。
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
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

  // runAction 统一处理按钮的处理中、成功和失败反馈，避免用户误以为点击无效。
  async function runAction(key: ActionKey, pending: string, success: string, operation: () => Promise<void>) {
    setPendingAction(key);
    setLastAction(null);
    setFeedback({ tone: "pending", message: pending });
    try {
      await Promise.all([operation(), delay(260)]);
      setFeedback({ tone: "success", message: success });
      setLastAction(key);
    } catch (error) {
      const message = error instanceof Error ? error.message : "操作失败";
      setFeedback({ tone: "error", message });
      appendLog(message);
    } finally {
      setPendingAction(null);
    }
  }

  // handleCreate 创建竞拍后立即选中新记录。
  async function handleCreate() {
    await runAction("create", "正在创建竞拍...", "竞拍创建成功", async () => {
      const created = await createAuction(form);
      appendLog(`已创建竞拍 ${created.id}`);
      await refreshAuctions();
      setSelectedId(created.id);
    });
  }

  // handleStart 触发后端初始化实时竞拍状态。
  async function handleStart() {
    if (!selectedId) return;
    await runAction("start", "正在启动竞拍...", "竞拍启动成功", async () => {
      const next = await startAuction(selectedId);
      setSnapshot(next);
      setBidAmount(next.nextMinimumBid);
      appendLog("竞拍已启动");
      await refreshAuctions();
    });
  }

  // handleCancel 演示主播异常取消竞拍。
  async function handleCancel() {
    if (!selectedId) return;
    await runAction("cancel", "正在取消竞拍...", "竞拍取消成功", async () => {
      const next = await cancelAuction(selectedId);
      setSnapshot(next);
      appendLog("竞拍已取消");
      await refreshAuctions();
    });
  }

  // handleFinish 演示自然结束或手动结算到期竞拍。
  async function handleFinish() {
    if (!selectedId) return;
    await runAction("finish", "正在结算竞拍...", "竞拍结算完成", async () => {
      const next = await finishAuction(selectedId);
      setSnapshot(next);
      appendLog(`竞拍结束：${next.status}`);
      await refreshAuctions();
    });
  }

  // handleBid 提交用户出价，并将下一口价反馈到输入框。
  async function handleBid() {
    if (!selectedId) return;
    await runAction("bid", "正在提交出价...", "出价提交成功", async () => {
      const bid = await placeBid(selectedId, userId, bidAmount);
      setSnapshot(bid.snapshot);
      setBidAmount(bid.snapshot.nextMinimumBid);
      appendLog(`出价成功：${bid.snapshot.currentPrice}`);
    });
  }

  // handleResult 展示后端返回的最终竞拍和订单摘要。
  async function handleResult() {
    if (!selectedId) return;
    await runAction("result", "正在查询结果...", "结果查询成功", async () => {
      const data = await getResult(selectedId);
      setResult(JSON.stringify(data, null, 2));
    });
  }

  const isBusy = pendingAction !== null;

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Realtime Auction Core</p>
          <h1>实时竞拍控制台</h1>
        </div>
        <div className={`action-feedback ${feedback.tone}`} role="status" aria-live="polite">
          <span>{feedback.tone === "idle" ? "当前状态" : "操作反馈"}</span>
          <strong>{feedback.message}</strong>
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
            <ActionButton actionKey="create" label="创建竞拍" pendingLabel="创建中..." successLabel="创建成功" pendingAction={pendingAction} lastAction={lastAction} onClick={handleCreate} disabled={isBusy} />
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
            <ActionButton actionKey="start" label="启动" pendingLabel="启动中..." successLabel="已启动" pendingAction={pendingAction} lastAction={lastAction} onClick={handleStart} disabled={!selectedId || isBusy} />
            <ActionButton actionKey="cancel" label="取消" pendingLabel="取消中..." successLabel="已取消" pendingAction={pendingAction} lastAction={lastAction} onClick={handleCancel} disabled={!selectedId || isBusy} />
            <ActionButton actionKey="finish" label="结算结束" pendingLabel="结算中..." successLabel="已结算" pendingAction={pendingAction} lastAction={lastAction} onClick={handleFinish} disabled={!selectedId || isBusy} />
            <ActionButton actionKey="result" label="查看结果" pendingLabel="查询中..." successLabel="已查询" pendingAction={pendingAction} lastAction={lastAction} onClick={handleResult} disabled={!selectedId || isBusy} />
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
            <Metric label="倒计时" value={remainingLabel} />
            <Metric label="参与人数" value={String(snapshot?.participants ?? 0)} />
            <Metric label="我的排名" value={snapshot?.rank ? `#${snapshot.rank}` : "-"} />
            <Metric label="最低出价" value={currency(snapshot?.nextMinimumBid ?? bidAmount)} />
          </div>

          <div className="bidline">
            <input value={userId} onChange={(e) => setUserId(e.target.value)} />
            <input type="number" value={bidAmount} onChange={(e) => setBidAmount(Number(e.target.value))} />
            <ActionButton actionKey="bid" label="立即出价" pendingLabel="出价中..." successLabel="出价成功" pendingAction={pendingAction} lastAction={lastAction} onClick={handleBid} disabled={!selectedId || isBusy} />
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

// ActionButton 根据当前操作状态切换按钮文案和颜色反馈。
function ActionButton({
  actionKey,
  label,
  pendingLabel,
  successLabel,
  pendingAction,
  lastAction,
  disabled,
  onClick
}: {
  actionKey: ActionKey;
  label: string;
  pendingLabel: string;
  successLabel: string;
  pendingAction: ActionKey | null;
  lastAction: ActionKey | null;
  disabled: boolean;
  onClick: () => void;
}) {
  const isPending = pendingAction === actionKey;
  const isSuccess = lastAction === actionKey;
  const className = ["action-button", isPending ? "is-pending" : "", isSuccess ? "is-success" : ""].filter(Boolean).join(" ");
  return (
    <button className={className} onClick={onClick} disabled={disabled || isPending} aria-busy={isPending}>
      {isPending ? pendingLabel : isSuccess ? successLabel : label}
    </button>
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

// formatRemaining 将结束时间转换成直播间可读的倒计时文本。
function formatRemaining(endsAt: string | undefined, now: number) {
  if (!endsAt) {
    return "-";
  }
  const remainingSeconds = Math.max(0, Math.ceil((Date.parse(endsAt) - now) / 1000));
  const minutes = Math.floor(remainingSeconds / 60);
  const seconds = remainingSeconds % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

// delay 让极快完成的请求也保留短暂处理中反馈。
function delay(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
