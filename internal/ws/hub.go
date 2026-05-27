// ws 提供按 auctionId 隔离的房间事件 Hub。
package ws

import (
	"sync"
	"time"

	"realtime-auction-core/internal/domain/auction"
)

type EventType string

const (
	// EventSnapshot 表示用户入房或状态刷新。
	EventSnapshot EventType = "snapshot"
	// EventBidAccepted 表示有效出价已被接受。
	EventBidAccepted EventType = "bidAccepted"
	// EventAuctionExtended 表示竞拍被自动延时。
	EventAuctionExtended EventType = "auctionExtended"
	// EventAuctionEnded 表示竞拍成交或自然结束。
	EventAuctionEnded EventType = "auctionEnded"
	// EventAuctionCancelled 表示竞拍被商家取消。
	EventAuctionCancelled EventType = "auctionCancelled"
)

// Event 是房间广播给前端的统一事件结构。
type Event struct {
	Type       EventType         `json:"type"`
	AuctionID  string            `json:"auctionId"`
	Snapshot   auction.Snapshot  `json:"snapshot"`
	Reason     string            `json:"reason,omitempty"`
	ServerTime time.Time         `json:"serverTime"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// Hub 管理多个竞拍房间及其订阅者。
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[chan Event]struct{}
}

// NewHub 创建空房间 Hub。
func NewHub() *Hub {
	return &Hub{rooms: map[string]map[chan Event]struct{}{}}
}

// Subscribe 订阅指定竞拍房间，并返回取消订阅函数。
func (h *Hub) Subscribe(auctionID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	if _, ok := h.rooms[auctionID]; !ok {
		h.rooms[auctionID] = map[chan Event]struct{}{}
	}
	h.rooms[auctionID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if room, ok := h.rooms[auctionID]; ok {
			delete(room, ch)
			if len(room) == 0 {
				delete(h.rooms, auctionID)
			}
		}
		close(ch)
	}
	return ch, cancel
}

// Broadcast 向指定竞拍房间广播事件，慢消费者会被跳过以保护主流程。
func (h *Hub) Broadcast(event Event) {
	event.ServerTime = time.Now().UTC()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.rooms[event.AuctionID] {
		select {
		case ch <- event:
		default:
		}
	}
}

// RoomSize 返回指定房间当前订阅者数量。
func (h *Hub) RoomSize(auctionID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.rooms[auctionID])
}
