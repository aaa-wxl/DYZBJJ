// ws 测试覆盖房间级事件隔离。
package ws

import (
	"testing"

	"realtime-auction-core/internal/domain/auction"
)

func TestHubBroadcastsOnlyToAuctionRoom(t *testing.T) {
	hub := NewHub()
	roomA, cancelA := hub.Subscribe("auction-a")
	defer cancelA()
	roomB, cancelB := hub.Subscribe("auction-b")
	defer cancelB()

	hub.Broadcast(Event{Type: EventBidAccepted, AuctionID: "auction-a", Snapshot: auction.Snapshot{AuctionID: "auction-a"}})

	select {
	case event := <-roomA:
		if event.AuctionID != "auction-a" {
			t.Fatalf("auction id = %s, want auction-a", event.AuctionID)
		}
	default:
		t.Fatal("expected room A to receive event")
	}

	select {
	case event := <-roomB:
		t.Fatalf("room B should not receive event: %+v", event)
	default:
	}
}
