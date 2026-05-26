// auction 测试覆盖竞拍规则和状态机的核心行为。
package auction

import (
	"testing"
	"time"
)

func TestRulesValidateRejectsInvalidIncrement(t *testing.T) {
	rules := Rules{
		StartPrice:      0,
		Increment:       0,
		Duration:        time.Minute,
		CeilingPrice:    1000,
		ExtendThreshold: 10 * time.Second,
		ExtendBy:        20 * time.Second,
	}

	if err := rules.Validate(); err == nil {
		t.Fatal("expected invalid increment to be rejected")
	}
}

func TestAuctionStartInitializesRunningState(t *testing.T) {
	a := newTestAuction()

	if a.Status != StatusDraft {
		t.Fatalf("new auction status = %s, want %s", a.Status, StatusDraft)
	}

	now := time.Unix(100, 0)
	if err := a.Start(now); err != nil {
		t.Fatalf("start auction: %v", err)
	}

	if a.Status != StatusRunning {
		t.Fatalf("status = %s, want %s", a.Status, StatusRunning)
	}
	if a.CurrentPrice != 0 {
		t.Fatalf("current price = %d, want 0", a.CurrentPrice)
	}
	if !a.EndsAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("ends at = %s, want %s", a.EndsAt, now.Add(time.Minute))
	}
}

func TestRunningAuctionCanBeCancelled(t *testing.T) {
	a := newTestAuction()
	if err := a.Start(time.Unix(100, 0)); err != nil {
		t.Fatalf("start auction: %v", err)
	}

	if err := a.Cancel(); err != nil {
		t.Fatalf("cancel running auction: %v", err)
	}

	if a.Status != StatusCancelled {
		t.Fatalf("status = %s, want %s", a.Status, StatusCancelled)
	}
}

func TestCancelRejectsFinishedAuction(t *testing.T) {
	a := Auction{Status: StatusSold}

	if err := a.Cancel(); err == nil {
		t.Fatal("expected sold auction cancellation to be rejected")
	}
}

func TestFinishSellsAuctionWithHighestBid(t *testing.T) {
	a := Auction{
		Status:        StatusRunning,
		CurrentPrice:  500,
		HighestBidder: "user-1",
		EndsAt:        time.Unix(100, 0),
	}

	if err := a.Finish(time.Unix(101, 0)); err != nil {
		t.Fatalf("finish auction: %v", err)
	}

	if a.Status != StatusSold {
		t.Fatalf("status = %s, want %s", a.Status, StatusSold)
	}
}

func TestFinishEndsAuctionWithoutHighestBid(t *testing.T) {
	a := Auction{
		Status: StatusRunning,
		EndsAt: time.Unix(100, 0),
	}

	if err := a.Finish(time.Unix(101, 0)); err != nil {
		t.Fatalf("finish auction: %v", err)
	}

	if a.Status != StatusEnded {
		t.Fatalf("status = %s, want %s", a.Status, StatusEnded)
	}
}

func TestTerminalAuctionsRejectCancelAndStart(t *testing.T) {
	for _, status := range []Status{StatusSold, StatusEnded, StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			a := newTestAuction()
			a.Status = status

			if err := a.Cancel(); err == nil {
				t.Fatalf("expected cancel in %s to be rejected", status)
			}
			if err := a.Start(time.Unix(100, 0)); err == nil {
				t.Fatalf("expected start in %s to be rejected", status)
			}
		})
	}
}

func TestScheduledAndExtendedAreNotSupportedAuctionStates(t *testing.T) {
	a := newTestAuction()
	a.Status = Status("SCHEDULED")
	if err := a.Start(time.Unix(100, 0)); err == nil {
		t.Fatal("expected scheduled auction start to be rejected")
	}

	a.Status = Status("EXTENDED")
	if err := a.Cancel(); err == nil {
		t.Fatal("expected extended auction cancellation to be rejected")
	}
	if err := a.Finish(time.Unix(101, 0)); err == nil {
		t.Fatal("expected extended auction finish to be rejected")
	}
	if IsOpenForBid(Status("EXTENDED")) {
		t.Fatal("expected extended auction to be closed for bids")
	}
}

func newTestAuction() Auction {
	return NewAuction("merchant-1", Product{Name: "jade", ImageURL: "https://example.com/jade.png", Description: "demo"}, Rules{
		StartPrice:      0,
		Increment:       100,
		Duration:        time.Minute,
		CeilingPrice:    1000,
		ExtendThreshold: 10 * time.Second,
		ExtendBy:        20 * time.Second,
	})
}
