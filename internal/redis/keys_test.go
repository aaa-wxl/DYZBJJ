package redis

import "testing"

func TestAuctionKeys(t *testing.T) {
	auctionID := "auc-1"

	tests := map[string]string{
		AuctionSnapshotKey(auctionID):       "auction:auc-1:snapshot",
		AuctionRankKey(auctionID):           "auction:auc-1:ranking",
		AuctionAmountKey(auctionID):         "auction:auc-1:amounts",
		AuctionRankSeqKey(auctionID):        "auction:auc-1:rank_seq",
		AuctionSeqKey(auctionID):            "auction:auc-1:seq",
		AuctionRequestKey(auctionID, "r-1"): "auction:auc-1:request:r-1",
		AuctionEventsKey(auctionID):         "auction:auc-1:events",
	}

	for got, want := range tests {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}
