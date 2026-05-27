package http

import (
	"bytes"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
	"realtime-auction-core/internal/ws"
)

func TestLoginWithPasswordAndRoleProtectedAdminCreate(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	bidder := loginWithPassword(t, ts, "userA", "123456", auction.RoleBidder)
	blocked := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), bidder.Token)
	if blocked.Code != nethttp.StatusForbidden {
		t.Fatalf("bidder admin create status = %d body=%s", blocked.Code, blocked.Body.String())
	}

	admin := loginWithPassword(t, ts, "admin", "admin123", auction.RoleAdmin)
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), admin.Token)
	if created.Code != nethttp.StatusCreated {
		t.Fatalf("admin create status = %d body=%s", created.Code, created.Body.String())
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	res := postJSON(t, ts, "/api/login", map[string]any{"username": "userA", "password": "bad-password"}, "")
	if res.Code != nethttp.StatusUnauthorized {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestBidStepErrorReturnsStructuredBody(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	admin := loginWithPassword(t, ts, "admin", "admin123", auction.RoleAdmin)
	bidder := loginWithPassword(t, ts, "userA", "123456", auction.RoleBidder)
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), admin.Token)
	if created.Code != nethttp.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var a auction.Auction
	decodeBody(t, created.Body.Bytes(), &a)
	started := postJSON(t, ts, "/api/admin/auctions/"+a.ID+"/start", nil, admin.Token)
	if started.Code != nethttp.StatusOK {
		t.Fatalf("start status = %d body=%s", started.Code, started.Body.String())
	}

	low := postJSON(t, ts, "/api/auctions/"+a.ID+"/bids", map[string]any{
		"requestId": "req-1",
		"amount":    101,
	}, bidder.Token)
	if low.Code != nethttp.StatusBadRequest {
		t.Fatalf("low bid status = %d body=%s", low.Code, low.Body.String())
	}
	var errBody map[string]any
	decodeBody(t, low.Body.Bytes(), &errBody)
	if errBody["code"] != "BID_STEP_INVALID" {
		t.Fatalf("error code = %#v, want BID_STEP_INVALID", errBody["code"])
	}
	if _, ok := errBody["message"].(string); !ok {
		t.Fatalf("missing message in %#v", errBody)
	}
	if _, ok := errBody["details"].(map[string]any); !ok {
		t.Fatalf("missing details in %#v", errBody)
	}
}

func TestBidUsesAuthenticatedUser(t *testing.T) {
	ts := newHTTPTestServer(t)
	defer ts.Close()

	admin := loginWithPassword(t, ts, "admin", "admin123", auction.RoleAdmin)
	bidder := loginWithPassword(t, ts, "userA", "123456", auction.RoleBidder)
	created := postJSON(t, ts, "/api/admin/auctions", validCreatePayload(), admin.Token)
	if created.Code != nethttp.StatusCreated {
		t.Fatalf("create status = %d body=%s", created.Code, created.Body.String())
	}
	var a auction.Auction
	decodeBody(t, created.Body.Bytes(), &a)
	postJSON(t, ts, "/api/admin/auctions/"+a.ID+"/start", nil, admin.Token)

	res := postJSON(t, ts, "/api/auctions/"+a.ID+"/bids", map[string]any{
		"userId":    "spoofed-user",
		"requestId": "req-auth-user",
		"amount":    100,
	}, bidder.Token)
	if res.Code != nethttp.StatusOK {
		t.Fatalf("bid status = %d body=%s", res.Code, res.Body.String())
	}
	var result redis.BidResult
	decodeBody(t, res.Body.Bytes(), &result)
	if result.Snapshot.HighestBidder != bidder.User.ID {
		t.Fatalf("highest bidder = %s, want authenticated user %s", result.Snapshot.HighestBidder, bidder.User.ID)
	}
}

type httpTestResponse struct {
	Code int
	Body *bytes.Buffer
}

func newHTTPTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repo := repository.NewMemoryRepository()
	store := redis.NewMemoryStore()
	hub := ws.NewHub()
	auctionService := service.NewAuctionService(repo, store, hub)
	authService := service.NewAuthService(repo, "test-secret", 24*time.Hour)
	if err := authService.SeedDemoUsers(); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewServer(auctionService, authService).Handler())
}

func loginWithPassword(t *testing.T, ts *httptest.Server, username, password string, role auction.Role) service.LoginSession {
	t.Helper()
	res := postJSON(t, ts, "/api/login", map[string]any{"username": username, "password": password}, "")
	if res.Code != nethttp.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	var session service.LoginSession
	decodeBody(t, res.Body.Bytes(), &session)
	if session.User.Role != role {
		t.Fatalf("login role = %s, want %s body=%s", session.User.Role, role, res.Body.String())
	}
	if session.User.Username != username {
		t.Fatalf("login username = %s, want %s", session.User.Username, username)
	}
	return session
}

func postJSON(t *testing.T, ts *httptest.Server, path string, payload any, token string) httpTestResponse {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := nethttp.NewRequest(nethttp.MethodPost, ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, res.Body); err != nil {
		t.Fatal(err)
	}
	return httpTestResponse{Code: res.StatusCode, Body: buf}
}

func decodeBody(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode body %s: %v", string(data), err)
	}
}

func validCreatePayload() map[string]any {
	return map[string]any{
		"merchantId":             "merchant-demo",
		"productName":            "星河翡翠手镯",
		"imageUrl":               "https://example.com/item.jpg",
		"description":            "演示竞拍商品",
		"startPrice":             0,
		"increment":              100,
		"durationSeconds":        int64(time.Minute / time.Second),
		"ceilingPrice":           3000,
		"extendThresholdSeconds": 20,
		"extendBySeconds":        30,
	}
}
