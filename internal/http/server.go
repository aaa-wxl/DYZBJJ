package http

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"time"

	"realtime-auction-core/internal/domain/auction"
	"realtime-auction-core/internal/redis"
	"realtime-auction-core/internal/repository"
	"realtime-auction-core/internal/service"
)

type Server struct {
	auction *service.AuctionService
	auth    *service.AuthService
	mux     *nethttp.ServeMux
}

func NewServer(auctionService *service.AuctionService, authService *service.AuthService) *Server {
	s := &Server{auction: auctionService, auth: authService, mux: nethttp.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() nethttp.Handler {
	return cors(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("GET /api/admin/auctions", s.adminListAuctions)
	s.mux.HandleFunc("POST /api/admin/auctions", s.adminCreateAuction)
	s.mux.HandleFunc("POST /api/admin/auctions/{id}/start", s.adminStartAuction)
	s.mux.HandleFunc("POST /api/admin/auctions/{id}/cancel", s.adminCancelAuction)
	s.mux.HandleFunc("GET /api/auctions", s.listAuctions)
	s.mux.HandleFunc("GET /api/auctions/{id}/snapshot", s.snapshot)
	s.mux.HandleFunc("POST /api/auctions/{id}/bids", s.placeBid)
	s.mux.HandleFunc("GET /api/auctions/{id}/result", s.result)
	s.mux.HandleFunc("GET /api/auctions/{id}/events", s.events)
	s.mux.HandleFunc("GET /ws/auctions/{id}", s.websocketEvents)
}

type loginRequest struct {
	Name string       `json:"name"`
	Role auction.Role `json:"role"`
}

func (s *Server) login(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "BAD_REQUEST", "请求体格式错误", nil)
		return
	}
	session, err := s.auth.Login(req.Name, req.Role)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, session)
}

type createAuctionRequest struct {
	MerchantID      string `json:"merchantId"`
	ProductName     string `json:"productName"`
	ImageURL        string `json:"imageUrl"`
	Description     string `json:"description"`
	StartPrice      int64  `json:"startPrice"`
	Increment       int64  `json:"increment"`
	DurationSeconds int64  `json:"durationSeconds"`
	CeilingPrice    int64  `json:"ceilingPrice"`
	ExtendThreshold int64  `json:"extendThresholdSeconds"`
	ExtendBy        int64  `json:"extendBySeconds"`
}

func (s *Server) adminListAuctions(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, ok := s.require(w, r, auction.RoleAdmin); !ok {
		return
	}
	s.writeAuctionList(w)
}

func (s *Server) listAuctions(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, ok := s.require(w, r, auction.RoleBidder); !ok {
		return
	}
	s.writeAuctionList(w)
}

func (s *Server) writeAuctionList(w nethttp.ResponseWriter) {
	items, err := s.auction.ListAuctions()
	if err != nil {
		writeAPIError(w, nethttp.StatusInternalServerError, "INTERNAL_ERROR", "竞拍列表读取失败", nil)
		return
	}
	writeJSON(w, nethttp.StatusOK, items)
}

func (s *Server) adminCreateAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, ok := s.require(w, r, auction.RoleAdmin); !ok {
		return
	}
	var req createAuctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "BAD_REQUEST", "请求体格式错误", nil)
		return
	}
	a, err := s.auction.CreateAuction(req.MerchantID, auction.Product{
		Name:        req.ProductName,
		ImageURL:    req.ImageURL,
		Description: req.Description,
	}, auction.Rules{
		StartPrice:      req.StartPrice,
		Increment:       req.Increment,
		Duration:        time.Duration(req.DurationSeconds) * time.Second,
		CeilingPrice:    req.CeilingPrice,
		ExtendThreshold: time.Duration(req.ExtendThreshold) * time.Second,
		ExtendBy:        time.Duration(req.ExtendBy) * time.Second,
	})
	if err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "INVALID_RULES", "竞拍规则不合法", nil)
		return
	}
	writeJSON(w, nethttp.StatusCreated, a)
}

func (s *Server) adminStartAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.require(w, r, auction.RoleAdmin)
	if !ok {
		return
	}
	snapshot, err := s.auction.StartAuctionBy(r.PathValue("id"), time.Now().UTC(), user)
	if err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "INVALID_STATE", "当前竞拍不能启动", nil)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

func (s *Server) adminCancelAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.require(w, r, auction.RoleAdmin)
	if !ok {
		return
	}
	snapshot, err := s.auction.CancelAuctionBy(r.PathValue("id"), time.Now().UTC(), user)
	if err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "INVALID_STATE", "当前竞拍不能取消", nil)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

func (s *Server) snapshot(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.require(w, r, auction.RoleBidder)
	if !ok {
		return
	}
	snapshot, err := s.auction.Snapshot(r.PathValue("id"), user.ID)
	if err != nil {
		writeAuctionLookupError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

type bidRequest struct {
	RequestID string `json:"requestId"`
	Amount    int64  `json:"amount"`
}

func (s *Server) placeBid(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.require(w, r, auction.RoleBidder)
	if !ok {
		return
	}
	var req bidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, nethttp.StatusBadRequest, "BAD_REQUEST", "请求体格式错误", nil)
		return
	}
	result, err := s.auction.PlaceBid(redis.BidCommand{
		AuctionID: r.PathValue("id"),
		UserID:    user.ID,
		UserName:  user.Name,
		RequestID: req.RequestID,
		Amount:    req.Amount,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		writeBidError(w, err, result)
		return
	}
	writeJSON(w, nethttp.StatusOK, result)
}

func (s *Server) result(w nethttp.ResponseWriter, r *nethttp.Request) {
	if _, ok := s.requireAny(w, r); !ok {
		return
	}
	result, err := s.auction.GetResult(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeAPIError(w, nethttp.StatusNotFound, "AUCTION_NOT_FOUND", "竞拍不存在", nil)
			return
		}
		writeAPIError(w, nethttp.StatusInternalServerError, "INTERNAL_ERROR", "竞拍结果读取失败", nil)
		return
	}
	writeJSON(w, nethttp.StatusOK, result)
}

func (s *Server) events(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.require(w, r, auction.RoleBidder)
	if !ok {
		return
	}
	auctionID := r.PathValue("id")
	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		writeAPIError(w, nethttp.StatusInternalServerError, "STREAM_UNSUPPORTED", "当前服务不支持事件流", nil)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.auction.Subscribe(auctionID)
	defer cancel()
	if snapshot, err := s.auction.Snapshot(auctionID, user.ID); err == nil {
		writeSSE(w, "snapshot", snapshot)
		flusher.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			writeSSE(w, string(event.Type), event)
			flusher.Flush()
		}
	}
}

func (s *Server) websocketEvents(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, ok := s.requireAnyToken(w, r.URL.Query().Get("token"))
	if !ok {
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		writeAPIError(w, nethttp.StatusBadRequest, "BAD_REQUEST", "缺少 WebSocket 握手信息", nil)
		return
	}
	hijacker, ok := w.(nethttp.Hijacker)
	if !ok {
		writeAPIError(w, nethttp.StatusInternalServerError, "WEBSOCKET_UNSUPPORTED", "当前服务不支持 WebSocket", nil)
		return
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	accept := websocketAccept(key)
	_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\n")
	_, _ = fmt.Fprintf(rw, "Upgrade: websocket\r\n")
	_, _ = fmt.Fprintf(rw, "Connection: Upgrade\r\n")
	_, _ = fmt.Fprintf(rw, "Sec-WebSocket-Accept: %s\r\n\r\n", accept)
	_ = rw.Flush()

	auctionID := r.PathValue("id")
	ch, cancel := s.auction.Subscribe(auctionID)
	defer cancel()
	if snapshot, err := s.auction.Snapshot(auctionID, user.ID); err == nil {
		if err := writeWebSocketJSON(conn, map[string]any{"type": "snapshot", "snapshot": snapshot}); err != nil {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if err := writeWebSocketJSON(conn, event); err != nil {
				return
			}
		}
	}
}

func (s *Server) require(w nethttp.ResponseWriter, r *nethttp.Request, role auction.Role) (auction.User, bool) {
	user, err := s.auth.Require(bearerToken(r), role)
	if err != nil {
		writeAuthError(w, err)
		return auction.User{}, false
	}
	return user, true
}

func (s *Server) requireAny(w nethttp.ResponseWriter, r *nethttp.Request) (auction.User, bool) {
	return s.requireAnyToken(w, bearerToken(r))
}

func (s *Server) requireAnyToken(w nethttp.ResponseWriter, token string) (auction.User, bool) {
	user, err := s.auth.Require(token, auction.RoleBidder)
	if err == nil {
		return user, true
	}
	user, err = s.auth.Require(token, auction.RoleAdmin)
	if err == nil {
		return user, true
	}
	writeAuthError(w, service.ErrUnauthorized)
	return auction.User{}, false
}

func bearerToken(r *nethttp.Request) string {
	value := r.Header.Get("Authorization")
	if strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	return ""
}

func writeAuthError(w nethttp.ResponseWriter, err error) {
	if errors.Is(err, service.ErrAuthStorage) {
		writeAPIError(w, nethttp.StatusInternalServerError, "AUTH_STORAGE_ERROR", "登录状态保存失败", nil)
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		writeAPIError(w, nethttp.StatusForbidden, "FORBIDDEN", "当前用户无权执行该操作", nil)
		return
	}
	writeAPIError(w, nethttp.StatusUnauthorized, "UNAUTHORIZED", "请先登录", nil)
}

func writeAuctionLookupError(w nethttp.ResponseWriter, err error) {
	if errors.Is(err, redis.ErrAuctionNotFound) || errors.Is(err, repository.ErrNotFound) {
		writeAPIError(w, nethttp.StatusNotFound, "AUCTION_NOT_FOUND", "竞拍不存在", nil)
		return
	}
	writeAPIError(w, nethttp.StatusBadRequest, "INVALID_STATE", "竞拍状态不可用", nil)
}

func writeBidError(w nethttp.ResponseWriter, err error, result redis.BidResult) {
	details := map[string]any{"nextMinimumBid": result.NextMinimum}
	message := err.Error()
	switch {
	case strings.Contains(message, "at least"):
		writeAPIError(w, nethttp.StatusBadRequest, "BID_TOO_LOW", "出价低于最低有效价", details)
	case strings.Contains(message, "increment"):
		writeAPIError(w, nethttp.StatusBadRequest, "BID_STEP_INVALID", "出价不符合加价幅度", details)
	case strings.Contains(message, "status") || strings.Contains(message, "ended"):
		writeAPIError(w, nethttp.StatusBadRequest, "INVALID_STATE", "当前竞拍不允许出价", details)
	default:
		writeAPIError(w, nethttp.StatusBadRequest, "BID_REJECTED", "出价失败", details)
	}
}

func writeSSE(w nethttp.ResponseWriter, event string, payload any) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\n", strings.ReplaceAll(event, "\n", ""))
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func writeWebSocketJSON(conn net.Conn, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeWebSocketText(conn, data)
}

func writeWebSocketText(w io.Writer, payload []byte) error {
	header := []byte{0x81}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127,
			byte(uint64(len(payload))>>56),
			byte(uint64(len(payload))>>48),
			byte(uint64(len(payload))>>40),
			byte(uint64(len(payload))>>32),
			byte(uint64(len(payload))>>24),
			byte(uint64(len(payload))>>16),
			byte(uint64(len(payload))>>8),
			byte(uint64(len(payload))),
		)
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeJSON(w nethttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w nethttp.ResponseWriter, status int, code, message string, details any) {
	payload := map[string]any{"code": code, "message": message}
	if details != nil {
		payload["details"] = details
	}
	writeJSON(w, status, payload)
}

func cors(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == nethttp.MethodOptions {
			w.WriteHeader(nethttp.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
