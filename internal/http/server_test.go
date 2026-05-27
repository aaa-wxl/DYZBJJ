// http 测试把 README 手工清单中的核心 API 闭环转成可重复验证。
package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

func TestAuctionHTTPChecklistCoreFlow(t *testing.T) {
	handler := newTestServer()

	created := postJSON[auction.Auction](t, handler, "/api/auctions", map[string]any{
		"merchantId":             "merchant-1",
		"productName":            "翡翠手镯",
		"imageUrl":               "https://example.com/jade.png",
		"description":            "演示商品",
		"startPrice":             0,
		"increment":              100,
		"durationSeconds":        1,
		"ceilingPrice":           300,
		"extendThresholdSeconds": 5,
		"extendBySeconds":        30,
	}, http.StatusCreated)
	if created.Status != auction.StatusDraft {
		t.Fatalf("created status = %s, want %s", created.Status, auction.StatusDraft)
	}

	started := postJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/start", nil, http.StatusOK)
	if started.Status != auction.StatusRunning {
		t.Fatalf("started status = %s, want %s", started.Status, auction.StatusRunning)
	}

	joined := getJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/snapshot?userId=user-1", http.StatusOK)
	if joined.CurrentPrice != 0 || joined.NextMinimumBid != 100 {
		t.Fatalf("joined snapshot current=%d next=%d, want current=0 next=100", joined.CurrentPrice, joined.NextMinimumBid)
	}

	firstBid := postJSON[redis.BidResult](t, handler, "/api/auctions/"+created.ID+"/bids", map[string]any{
		"userId":    "user-1",
		"requestId": "req-1",
		"amount":    100,
	}, http.StatusOK)
	if firstBid.Snapshot.CurrentPrice != 100 || firstBid.Snapshot.Rank != 1 {
		t.Fatalf("first bid current=%d rank=%d, want current=100 rank=1", firstBid.Snapshot.CurrentPrice, firstBid.Snapshot.Rank)
	}
	if firstBid.Snapshot.Status != auction.StatusExtended {
		t.Fatalf("first bid status = %s, want %s", firstBid.Snapshot.Status, auction.StatusExtended)
	}

	rejoined := getJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/snapshot?userId=user-1", http.StatusOK)
	if rejoined.CurrentPrice != 100 || rejoined.NextMinimumBid != 200 || rejoined.Rank != 1 {
		t.Fatalf("rejoined snapshot current=%d next=%d rank=%d, want current=100 next=200 rank=1", rejoined.CurrentPrice, rejoined.NextMinimumBid, rejoined.Rank)
	}

	lowBid := postJSON[map[string]any](t, handler, "/api/auctions/"+created.ID+"/bids", map[string]any{
		"userId":    "user-2",
		"requestId": "req-low",
		"amount":    150,
	}, http.StatusBadRequest)
	if lowBid["nextMinimum"].(float64) != 200 {
		t.Fatalf("low bid nextMinimum = %v, want 200", lowBid["nextMinimum"])
	}

	soldBid := postJSON[redis.BidResult](t, handler, "/api/auctions/"+created.ID+"/bids", map[string]any{
		"userId":    "user-2",
		"requestId": "req-sold",
		"amount":    300,
	}, http.StatusOK)
	if soldBid.Snapshot.Status != auction.StatusSold {
		t.Fatalf("sold bid status = %s, want %s", soldBid.Snapshot.Status, auction.StatusSold)
	}

	result := getJSON[map[string]any](t, handler, "/api/auctions/"+created.ID+"/result", http.StatusOK)
	if result["hasOrder"] != true {
		t.Fatalf("result hasOrder = %v, want true", result["hasOrder"])
	}
}

func TestAuctionHTTPCancelFlow(t *testing.T) {
	handler := newTestServer()
	created := postJSON[auction.Auction](t, handler, "/api/auctions", validAuctionPayload("可取消竞拍", 1000), http.StatusCreated)
	_ = postJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/start", nil, http.StatusOK)

	cancelled := postJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/cancel", nil, http.StatusOK)
	if cancelled.Status != auction.StatusCancelled {
		t.Fatalf("cancelled status = %s, want %s", cancelled.Status, auction.StatusCancelled)
	}
}

func TestAuctionHTTPBidResultUsesFrontendJSONNames(t *testing.T) {
	handler := newTestServer()
	created := postJSON[auction.Auction](t, handler, "/api/auctions", validAuctionPayload("前端出价契约", 1000), http.StatusCreated)
	_ = postJSON[auction.Snapshot](t, handler, "/api/auctions/"+created.ID+"/start", nil, http.StatusOK)

	result := postJSON[map[string]any](t, handler, "/api/auctions/"+created.ID+"/bids", map[string]any{
		"userId":    "user-1",
		"requestId": "req-json-contract",
		"amount":    100,
	}, http.StatusOK)

	if _, ok := result["snapshot"]; !ok {
		t.Fatalf("bid result missing lowercase snapshot field: %#v", result)
	}
	if _, ok := result["Snapshot"]; ok {
		t.Fatalf("bid result should not expose uppercase Snapshot field: %#v", result)
	}
	if _, ok := result["nextMinimum"]; !ok {
		t.Fatalf("bid result missing lowercase nextMinimum field: %#v", result)
	}
}

func TestAuctionHTTPNaturalFinishFlow(t *testing.T) {
	handler := newTestServer()

	sold := postJSON[auction.Auction](t, handler, "/api/auctions", expiringAuctionPayload("自然成交", 1000), http.StatusCreated)
	_ = postJSON[auction.Snapshot](t, handler, "/api/auctions/"+sold.ID+"/start", nil, http.StatusOK)
	_ = postJSON[redis.BidResult](t, handler, "/api/auctions/"+sold.ID+"/bids", map[string]any{
		"userId":    "user-1",
		"requestId": "req-natural-sold",
		"amount":    100,
	}, http.StatusOK)

	ended := postJSON[auction.Auction](t, handler, "/api/auctions", expiringAuctionPayload("自然流拍", 1000), http.StatusCreated)
	_ = postJSON[auction.Snapshot](t, handler, "/api/auctions/"+ended.ID+"/start", nil, http.StatusOK)

	time.Sleep(1100 * time.Millisecond)

	finishedSold := postJSON[auction.Snapshot](t, handler, "/api/auctions/"+sold.ID+"/finish", nil, http.StatusOK)
	if finishedSold.Status != auction.StatusSold {
		t.Fatalf("finished sold status = %s, want %s", finishedSold.Status, auction.StatusSold)
	}
	soldResult := getJSON[map[string]any](t, handler, "/api/auctions/"+sold.ID+"/result", http.StatusOK)
	if soldResult["hasOrder"] != true {
		t.Fatalf("sold result hasOrder = %v, want true", soldResult["hasOrder"])
	}

	finishedEnded := postJSON[auction.Snapshot](t, handler, "/api/auctions/"+ended.ID+"/finish", nil, http.StatusOK)
	if finishedEnded.Status != auction.StatusEnded {
		t.Fatalf("finished ended status = %s, want %s", finishedEnded.Status, auction.StatusEnded)
	}
	endedResult := getJSON[map[string]any](t, handler, "/api/auctions/"+ended.ID+"/result", http.StatusOK)
	if endedResult["hasOrder"] != false {
		t.Fatalf("ended result hasOrder = %v, want false", endedResult["hasOrder"])
	}
}

func newTestServer() http.Handler {
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	return NewServer(service.NewAuctionService(repo, store, hub)).Handler()
}

func validAuctionPayload(name string, ceiling int64) map[string]any {
	return map[string]any{
		"merchantId":             "merchant-1",
		"productName":            name,
		"imageUrl":               "https://example.com/item.png",
		"description":            "演示商品",
		"startPrice":             0,
		"increment":              100,
		"durationSeconds":        60,
		"ceilingPrice":           ceiling,
		"extendThresholdSeconds": 5,
		"extendBySeconds":        30,
	}
}

func expiringAuctionPayload(name string, ceiling int64) map[string]any {
	payload := validAuctionPayload(name, ceiling)
	payload["durationSeconds"] = 1
	payload["extendThresholdSeconds"] = 0
	payload["extendBySeconds"] = 0
	return payload
}

func postJSON[T any](t *testing.T, handler http.Handler, path string, body any, wantStatus int) T {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	return doJSON[T](t, handler, req, wantStatus)
}

func getJSON[T any](t *testing.T, handler http.Handler, path string, wantStatus int) T {
	t.Helper()
	return doJSON[T](t, handler, httptest.NewRequest(http.MethodGet, path, nil), wantStatus)
}

func doJSON[T any](t *testing.T, handler http.Handler, req *http.Request, wantStatus int) T {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s status = %d body=%s, want %d", req.Method, req.URL.Path, rec.Code, rec.Body.String(), wantStatus)
	}
	var out T
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	return out
}
