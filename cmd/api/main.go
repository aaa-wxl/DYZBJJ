package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	nethttp "net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/config"
	apphttp "realtime-auction-core/internal/http"
	rtredis "realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

var errRedisUnavailable = errors.New("redis unavailable")

type realtimeRuntime struct {
	Store       rtredis.Store
	RedisClient *goredis.Client
}

func (r realtimeRuntime) Close() {
	if r.RedisClient != nil {
		_ = r.RedisClient.Close()
	}
}

func main() {
	cfg := config.Load()
	repo, err := repository.NewFileRepository("data")
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	runtime, err := configureRealtimeStore(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close()

	hub := ws.NewHub()
	auctionService := service.NewAuctionService(repo, runtime.Store, hub)
	if runtime.RedisClient != nil {
		bus := ws.NewRedisBus(runtime.RedisClient, "api-local")
		auctionService.SetEventPublisher(bus)
		events, stop, err := bus.Subscribe(context.Background())
		if err != nil {
			log.Printf("redis event subscribe disabled: %v", err)
		} else {
			defer stop()
			go func() {
				for event := range events {
					auctionService.BroadcastExternal(event)
				}
			}()
		}
	}

	authService := service.NewAuthService(repo, cfg.JWTSecret, cfg.JWTTTL)
	if err := authService.SeedDemoUsers(); err != nil {
		log.Fatal(err)
	}
	server := apphttp.NewServer(auctionService, authService)

	log.Printf("auction api listening on %s", cfg.HTTPAddr)
	if err := nethttp.ListenAndServe(cfg.HTTPAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}

func configureRealtimeStore(ctx context.Context, cfg config.Config) (realtimeRuntime, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		if cfg.RedisRequired {
			return realtimeRuntime{}, fmt.Errorf("%w at %s: %v; run docker compose up -d redis mysql", errRedisUnavailable, cfg.RedisAddr, err)
		}
		log.Printf("redis unavailable at %s, using memory store because REDIS_REQUIRED=false: %v", cfg.RedisAddr, err)
		return realtimeRuntime{Store: rtredis.NewMemoryStore()}, nil
	}
	return realtimeRuntime{
		Store:       rtredis.NewRedisStore(client),
		RedisClient: client,
	}, nil
}
