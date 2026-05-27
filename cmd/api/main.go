package main

import (
	"log"
	nethttp "net/http"

	"realtime-auction-core/internal/config"
	apphttp "realtime-auction-core/internal/http"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

func main() {
	cfg := config.Load()
	repo, err := repository.NewFileRepository("data")
	if err != nil {
		log.Fatal(err)
	}
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	auctionService := service.NewAuctionService(repo, store, hub)
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
