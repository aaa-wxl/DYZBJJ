// api 启动竞拍系统的本地 HTTP/WebSocket 服务。
package main

import (
	"log"
	nethttp "net/http"

	apphttp "realtime-auction-core/internal/http"
	"realtime-auction-core/internal/config"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

func main() {
	cfg := config.Load()
	// 当前实现使用内存 repository 和内存 Redis-like store，便于先验证核心闭环。
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	auctionService := service.NewAuctionService(repo, store, hub)
	server := apphttp.NewServer(auctionService)

	log.Printf("auction api listening on %s", cfg.HTTPAddr)
	if err := nethttp.ListenAndServe(cfg.HTTPAddr, server.Handler()); err != nil {
		log.Fatal(err)
	}
}
