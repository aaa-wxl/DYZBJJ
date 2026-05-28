package ws

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

func TestRedisBusIgnoresOwnEvents(t *testing.T) {
	event := Event{Type: EventBidAccepted, AuctionID: "auc-1"}
	if shouldForwardRedisEvent("api-1", redisEventEnvelope{SourceID: "api-1", Event: event}) {
		t.Fatal("own event should not be forwarded")
	}
}

func TestRedisBusForwardsOtherInstanceEvents(t *testing.T) {
	event := Event{Type: EventBidAccepted, AuctionID: "auc-1"}
	if !shouldForwardRedisEvent("api-1", redisEventEnvelope{SourceID: "api-2", Event: event}) {
		t.Fatal("other instance event should be forwarded")
	}
}

func TestRedisBusPublishAndSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("requires local redis")
	}
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	defer client.Close()

	bus := NewRedisBus(client, "api-1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events, stop, err := bus.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	other := NewRedisBus(client, "api-2")
	if err := other.Publish(ctx, Event{Type: EventBidAccepted, AuctionID: "auc-1"}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-events:
		if event.AuctionID != "auc-1" || event.Type != EventBidAccepted {
			t.Fatalf("event = %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for redis event")
	}
}
