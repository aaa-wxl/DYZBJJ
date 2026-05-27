import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  adminCancelAuction,
  adminCreateAuction,
  adminListAuctions,
  adminStartAuction,
  type Auction,
  type CreateAuctionPayload,
  type LoginSession
} from "./api";

type AdminAppProps = {
  session: LoginSession;
  onLogout: () => void;
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
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const selected = useMemo(() => auctions.find((item) => item.id === selectedId), [auctions, selectedId]);

  useEffect(() => {
    void refresh();
  }, []);

  async function refresh(preferredId = selectedId) {
    const items = await adminListAuctions(session.token);
    setAuctions(items);
    setSelectedId(preferredId || items[0]?.id || "");
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    await run("竞拍已创建", async () => {
      const created = await adminCreateAuction(session.token, form);
      await refresh(created.id);
    });
  }

  async function startAuction() {
    if (!selected) return;
    await run("竞拍已启动", async () => {
      await adminStartAuction(session.token, selected.id);
      await refresh(selected.id);
    });
  }

  async function cancelAuction() {
    if (!selected) return;
    await run("竞拍已取消", async () => {
      await adminCancelAuction(session.token, selected.id);
      await refresh(selected.id);
    });
  }

  async function run(success: string, action: () => Promise<void>) {
    setLoading(true);
    setMessage("");
    try {
      await action();
      setMessage(success);
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "操作失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="admin-shell">
      <aside className="admin-sidebar">
        <div className="brand-block">
          <p className="eyebrow">Admin Console</p>
          <h1>竞拍管理</h1>
          <span>{session.user.name}</span>
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
            <p className="eyebrow">Current Lot</p>
            <h2>{selected?.product.name || "新建竞拍"}</h2>
          </div>
          {selected && <strong>{currency(selected.currentPrice)}</strong>}
        </header>

        <div className="admin-grid">
          <form className="auction-form" onSubmit={submit}>
            <h3>发布竞拍</h3>
            <label>
              商品名称
              <input value={form.productName} onChange={(event) => setForm({ ...form, productName: event.target.value })} />
            </label>
            <label>
              商品图片
              <input value={form.imageUrl} onChange={(event) => setForm({ ...form, imageUrl: event.target.value })} />
            </label>
            <label className="wide">
              商品介绍
              <textarea value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
            </label>
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
                  <StatusPill status={selected.status} />
                  <p>{selected.product.description}</p>
                  <dl>
                    <div><dt>起拍价</dt><dd>{currency(selected.rules.startPrice)}</dd></div>
                    <div><dt>加价幅度</dt><dd>{currency(selected.rules.increment)}</dd></div>
                    <div><dt>封顶价</dt><dd>{currency(selected.rules.ceilingPrice)}</dd></div>
                    <div><dt>结束时间</dt><dd>{selected.endsAt ? formatTime(selected.endsAt) : "-"}</dd></div>
                  </dl>
                </div>
                <div className="admin-actions">
                  <button onClick={startAuction} disabled={loading || selected.status !== "DRAFT"}>启动</button>
                  <button className="danger-btn" onClick={cancelAuction} disabled={loading || !["DRAFT", "RUNNING"].includes(selected.status)}>取消</button>
                  <button className="ghost-btn" onClick={() => void refresh(selected.id)} disabled={loading}>刷新</button>
                </div>
              </>
            ) : (
              <div className="empty-state">暂无竞拍</div>
            )}
            {message && <p className="notice-line">{message}</p>}
          </section>
        </div>
      </section>
    </main>
  );
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return (
    <label>
      {label}
      <input type="number" value={value} onChange={(event) => onChange(Number(event.target.value))} />
    </label>
  );
}

function StatusPill({ status }: { status: Auction["status"] }) {
  return <em className={`status-pill ${status.toLowerCase()}`}>{status}</em>;
}

function currency(value: number) {
  return `￥${value.toLocaleString("zh-CN")}`;
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(value));
}
