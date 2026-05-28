package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const redisEventChannel = "auction:events"

type RedisBus struct {
	client   *goredis.Client
	sourceID string
}

type redisEventEnvelope struct {
	SourceID string `json:"sourceId"`
	Event    Event  `json:"event"`
}

func NewRedisBus(client *goredis.Client, sourceID string) *RedisBus {
	return &RedisBus{client: client, sourceID: sourceID}
}

func (b *RedisBus) Publish(ctx context.Context, event Event) error {
	body, err := json.Marshal(redisEventEnvelope{SourceID: b.sourceID, Event: event})
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, redisEventChannel, body).Err()
}

func (b *RedisBus) Subscribe(ctx context.Context) (<-chan Event, func(), error) {
	pubsub := b.client.Subscribe(ctx, redisEventChannel)
	if _, err := pubsub.ReceiveTimeout(ctx, 3*time.Second); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}

	out := make(chan Event, 16)
	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(out)
		ch := pubsub.Channel()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if msg == nil {
					continue
				}
				envelope := redisEventEnvelope{}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					continue
				}
				if shouldForwardRedisEvent(b.sourceID, envelope) {
					select {
					case out <- envelope.Event:
					case <-done:
						return
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	stop := func() {
		once.Do(func() {
			close(done)
			_ = pubsub.Close()
		})
	}
	return out, stop, nil
}

func shouldForwardRedisEvent(sourceID string, envelope redisEventEnvelope) bool {
	return envelope.SourceID != "" && envelope.SourceID != sourceID && envelope.Event.AuctionID != ""
}
