// redis 集中维护竞拍实时状态在 Redis 中的 key 命名约定。
package redis

import "fmt"

func AuctionSnapshotKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:snapshot", auctionID)
}

func AuctionRankKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:ranking", auctionID)
}

func AuctionAmountKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:amounts", auctionID)
}

func AuctionRankSeqKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:rank_seq", auctionID)
}

func AuctionSeqKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:seq", auctionID)
}

func AuctionRequestKey(auctionID, requestID string) string {
	return fmt.Sprintf("auction:%s:request:%s", auctionID, requestID)
}

func AuctionEventsKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:events", auctionID)
}

// AuctionRoomKey 兼容旧的房间命名调用；新事件通道使用 AuctionEventsKey。
func AuctionRoomKey(auctionID string) string {
	return AuctionEventsKey(auctionID)
}
