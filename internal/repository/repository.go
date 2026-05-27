// repository 定义竞拍系统的持久化边界。
package repository

import (
	"errors"

	"realtime-auction-core/internal/domain/auction"
)

var ErrNotFound = errors.New("record not found")

type AuctionRepository interface {
	SaveUser(auction.User) error
	UpsertUser(auction.User) error
	SaveSession(auction.Session) error
	GetUserByToken(token string) (auction.User, error)
	GetUser(id string) (auction.User, error)
	GetUserByUsername(username string) (auction.User, error)
	ListUsers() ([]auction.User, error)
	CreateAuction(auction.Auction) (auction.Auction, error)
	UpdateAuction(auction.Auction) error
	GetAuction(id string) (auction.Auction, error)
	ListAuctions() ([]auction.Auction, error)
	SaveBid(auction.Bid) error
	ListBids(auctionID string) ([]auction.Bid, error)
	UpsertOrder(auction.Order) (auction.Order, error)
	GetOrderByAuction(auctionID string) (auction.Order, error)
}
