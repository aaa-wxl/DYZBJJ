// redis 集中维护竞拍实时状态在 Redis 中的 key 命名约定。
package redis

import "fmt"

func AuctionSnapshotKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:snapshot", auctionID)
}

// AuctionRankKey 保存单个竞拍房间的排行榜数据。
func AuctionRankKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:rank", auctionID)
}

// AuctionRequestKey 保存出价 requestId 的幂等处理结果。
func AuctionRequestKey(auctionID, requestID string) string {
	return fmt.Sprintf("auction:%s:req:%s", auctionID, requestID)
}

// AuctionRoomKey 表示竞拍房间事件通道。
func AuctionRoomKey(auctionID string) string {
	return fmt.Sprintf("auction:%s:room", auctionID)
}
