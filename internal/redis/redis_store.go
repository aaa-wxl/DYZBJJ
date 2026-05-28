package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"realtime-auction-core/internal/domain/auction"
)

const (
	requestTTL = 10 * time.Minute
	stateTTL   = 24 * time.Hour
)

type RedisStore struct {
	client *goredis.Client
}

func NewRedisStore(client *goredis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) InitAuction(snapshot auction.Snapshot) error {
	if snapshot.AuctionID == "" {
		return fmt.Errorf("auction id is required")
	}
	if err := snapshot.Rules.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	if snapshot.ServerTime.IsZero() {
		snapshot.ServerTime = time.Now().UTC()
	}
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	productJSON, err := json.Marshal(snapshot.Product)
	if err != nil {
		return err
	}

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, AuctionSnapshotKey(snapshot.AuctionID), map[string]any{
		"auctionId":         snapshot.AuctionID,
		"productJson":       string(productJSON),
		"status":            string(snapshot.Status),
		"currentPrice":      snapshot.CurrentPrice,
		"highestBidder":     snapshot.HighestBidder,
		"endsAtUnixMs":      snapshot.EndsAt.UnixMilli(),
		"serverTimeUnixMs":  snapshot.ServerTime.UnixMilli(),
		"startPrice":        snapshot.Rules.StartPrice,
		"increment":         snapshot.Rules.Increment,
		"durationMs":        snapshot.Rules.Duration.Milliseconds(),
		"ceilingPrice":      snapshot.Rules.CeilingPrice,
		"extendThresholdMs": snapshot.Rules.ExtendThreshold.Milliseconds(),
		"extendByMs":        snapshot.Rules.ExtendBy.Milliseconds(),
	})
	pipe.Del(ctx, AuctionRankKey(snapshot.AuctionID), AuctionAmountKey(snapshot.AuctionID), AuctionRankSeqKey(snapshot.AuctionID), AuctionSeqKey(snapshot.AuctionID))
	pipe.Expire(ctx, AuctionSnapshotKey(snapshot.AuctionID), stateTTL)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) Snapshot(auctionID, userID string) (auction.Snapshot, error) {
	ctx := context.Background()
	snapshot, err := s.loadSnapshot(ctx, auctionID)
	if err != nil {
		return auction.Snapshot{}, err
	}
	snapshot.ServerTime = time.Now().UTC()
	snapshot.NextMinimumBid = auction.NextMinimumBid(snapshot.CurrentPrice, snapshot.Rules)
	if err := s.fillRanking(ctx, &snapshot, userID); err != nil {
		return auction.Snapshot{}, err
	}
	return snapshot, nil
}

type bidScriptResult struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error"`
	BidID            string         `json:"bidId"`
	Idempotent       bool           `json:"idempotent"`
	Extended         bool           `json:"extended"`
	Status           auction.Status `json:"status"`
	CurrentPrice     int64          `json:"currentPrice"`
	HighestBidder    string         `json:"highestBidder"`
	EndsAtUnixMs     int64          `json:"endsAtUnixMs"`
	ServerTimeUnixMs int64          `json:"serverTimeUnixMs"`
	NextMinimum      int64          `json:"nextMinimum"`
}

func (s *RedisStore) PlaceBid(command BidCommand) (BidResult, error) {
	if command.UserID == "" || command.RequestID == "" {
		return BidResult{}, fmt.Errorf("%w: user id and request id are required", ErrBidRejected)
	}
	if command.Now.IsZero() {
		command.Now = time.Now().UTC()
	}

	ctx := context.Background()
	bidID := auction.NewID("bid")
	raw, err := s.client.Eval(ctx, placeBidScript, []string{
		AuctionSnapshotKey(command.AuctionID),
		AuctionRankKey(command.AuctionID),
		AuctionAmountKey(command.AuctionID),
		AuctionRankSeqKey(command.AuctionID),
		AuctionSeqKey(command.AuctionID),
		AuctionRequestKey(command.AuctionID, command.RequestID),
	}, command.AuctionID, command.UserID, command.RequestID, command.Amount, command.Now.UTC().UnixMilli(), bidID, int(requestTTL.Seconds())).Text()
	if err != nil {
		return BidResult{}, err
	}

	parsed := bidScriptResult{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return BidResult{}, err
	}
	if !parsed.OK {
		return BidResult{NextMinimum: parsed.NextMinimum}, scriptBidError(parsed)
	}

	snapshot, err := s.Snapshot(command.AuctionID, command.UserID)
	if err != nil {
		return BidResult{}, err
	}
	return BidResult{
		BidID:       parsed.BidID,
		Snapshot:    snapshot,
		NextMinimum: parsed.NextMinimum,
		Extended:    parsed.Extended,
		Idempotent:  parsed.Idempotent,
	}, nil
}

func (s *RedisStore) loadSnapshot(ctx context.Context, auctionID string) (auction.Snapshot, error) {
	values, err := s.client.HGetAll(ctx, AuctionSnapshotKey(auctionID)).Result()
	if err != nil {
		return auction.Snapshot{}, err
	}
	if len(values) == 0 {
		return auction.Snapshot{}, ErrAuctionNotFound
	}

	product := auction.Product{}
	if err := json.Unmarshal([]byte(values["productJson"]), &product); err != nil {
		return auction.Snapshot{}, err
	}
	currentPrice, err := parseInt(values, "currentPrice")
	if err != nil {
		return auction.Snapshot{}, err
	}
	endsAtMs, err := parseInt(values, "endsAtUnixMs")
	if err != nil {
		return auction.Snapshot{}, err
	}
	serverTimeMs, err := parseInt(values, "serverTimeUnixMs")
	if err != nil {
		return auction.Snapshot{}, err
	}
	rules, err := parseRules(values)
	if err != nil {
		return auction.Snapshot{}, err
	}

	return auction.Snapshot{
		AuctionID:     values["auctionId"],
		Product:       product,
		Rules:         rules,
		Status:        auction.Status(values["status"]),
		CurrentPrice:  currentPrice,
		HighestBidder: values["highestBidder"],
		EndsAt:        time.UnixMilli(endsAtMs).UTC(),
		ServerTime:    time.UnixMilli(serverTimeMs).UTC(),
	}, nil
}

func (s *RedisStore) fillRanking(ctx context.Context, snapshot *auction.Snapshot, userID string) error {
	rows, err := s.client.ZRevRangeWithScores(ctx, AuctionRankKey(snapshot.AuctionID), 0, 4).Result()
	if err != nil {
		return err
	}
	snapshot.Leaderboard = snapshot.Leaderboard[:0]
	for i, row := range rows {
		userID, ok := row.Member.(string)
		if !ok {
			return fmt.Errorf("invalid ranking member %T", row.Member)
		}
		amount, err := s.client.HGet(ctx, AuctionAmountKey(snapshot.AuctionID), userID).Int64()
		if err != nil {
			return err
		}
		snapshot.Leaderboard = append(snapshot.Leaderboard, auction.LeaderboardEntry{
			Rank:   i + 1,
			UserID: userID,
			Amount: amount,
		})
	}

	count, err := s.client.ZCard(ctx, AuctionRankKey(snapshot.AuctionID)).Result()
	if err != nil {
		return err
	}
	snapshot.Participants = int(count)
	snapshot.Rank = 0
	if userID != "" {
		rank, err := s.client.ZRevRank(ctx, AuctionRankKey(snapshot.AuctionID), userID).Result()
		if err == nil {
			snapshot.Rank = int(rank) + 1
		} else if err != goredis.Nil {
			return err
		}
	}
	return nil
}

func parseRules(values map[string]string) (auction.Rules, error) {
	startPrice, err := parseInt(values, "startPrice")
	if err != nil {
		return auction.Rules{}, err
	}
	increment, err := parseInt(values, "increment")
	if err != nil {
		return auction.Rules{}, err
	}
	durationMs, err := parseInt(values, "durationMs")
	if err != nil {
		return auction.Rules{}, err
	}
	ceilingPrice, err := parseInt(values, "ceilingPrice")
	if err != nil {
		return auction.Rules{}, err
	}
	extendThresholdMs, err := parseInt(values, "extendThresholdMs")
	if err != nil {
		return auction.Rules{}, err
	}
	extendByMs, err := parseInt(values, "extendByMs")
	if err != nil {
		return auction.Rules{}, err
	}
	return auction.Rules{
		StartPrice:      startPrice,
		Increment:       increment,
		Duration:        time.Duration(durationMs) * time.Millisecond,
		CeilingPrice:    ceilingPrice,
		ExtendThreshold: time.Duration(extendThresholdMs) * time.Millisecond,
		ExtendBy:        time.Duration(extendByMs) * time.Millisecond,
	}, nil
}

func parseInt(values map[string]string, key string) (int64, error) {
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("missing snapshot field %s", key)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot field %s: %w", key, err)
	}
	return parsed, nil
}

func scriptBidError(result bidScriptResult) error {
	switch result.Error {
	case "not_found":
		return ErrAuctionNotFound
	case "status":
		return fmt.Errorf("%w: auction status is %s", ErrBidRejected, result.Status)
	case "expired":
		return fmt.Errorf("%w: auction already ended", ErrBidRejected)
	case "low_bid":
		return fmt.Errorf("%w: amount must be at least %d", ErrBidRejected, result.NextMinimum)
	case "increment":
		return fmt.Errorf("%w: amount must follow increment", ErrBidRejected)
	default:
		return fmt.Errorf("%w: redis bid rejected", ErrBidRejected)
	}
}
