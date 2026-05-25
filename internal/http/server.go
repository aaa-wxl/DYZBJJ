// http 提供竞拍系统的 REST API、SSE 和最小 WebSocket 入口。
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
	service *service.AuctionService
	mux     *nethttp.ServeMux
}

// NewServer 注册所有本地演示需要的 HTTP 路由。
func NewServer(service *service.AuctionService) *Server {
	s := &Server{service: service, mux: nethttp.NewServeMux()}
	s.routes()
	return s
}

// Handler 返回带基础 CORS 支持的 HTTP handler。
func (s *Server) Handler() nethttp.Handler {
	return cors(s.mux)
}

// routes 将管理端、用户端和实时通信接口集中注册到 ServeMux。
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/auctions", s.listAuctions)
	s.mux.HandleFunc("POST /api/auctions", s.createAuction)
	s.mux.HandleFunc("POST /api/auctions/{id}/start", s.startAuction)
	s.mux.HandleFunc("POST /api/auctions/{id}/cancel", s.cancelAuction)
	s.mux.HandleFunc("GET /api/auctions/{id}/snapshot", s.snapshot)
	s.mux.HandleFunc("POST /api/auctions/{id}/bids", s.placeBid)
	s.mux.HandleFunc("POST /api/auctions/{id}/finish", s.finishAuction)
	s.mux.HandleFunc("GET /api/auctions/{id}/result", s.result)
	s.mux.HandleFunc("GET /api/auctions/{id}/events", s.events)
	s.mux.HandleFunc("GET /ws/auctions/{id}", s.websocketEvents)
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

func (s *Server) listAuctions(w nethttp.ResponseWriter, r *nethttp.Request) {
	items, err := s.service.ListAuctions()
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, items)
}

// createAuction 处理商家创建竞拍请求。
func (s *Server) createAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req createAuctionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	a, err := s.service.CreateAuction(req.MerchantID, auction.Product{
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
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, a)
}

// startAuction 将 DRAFT/SCHEDULED 竞拍启动为 RUNNING。
func (s *Server) startAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	snapshot, err := s.service.StartAuction(r.PathValue("id"), time.Now().UTC())
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

// cancelAuction 处理商家异常取消竞拍。
func (s *Server) cancelAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	snapshot, err := s.service.CancelAuction(r.PathValue("id"), time.Now().UTC())
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

// snapshot 返回用户进入房间或重连后的竞拍快照。
func (s *Server) snapshot(w nethttp.ResponseWriter, r *nethttp.Request) {
	userID := r.URL.Query().Get("userId")
	snapshot, err := s.service.Snapshot(r.PathValue("id"), userID)
	if err != nil {
		status := nethttp.StatusNotFound
		if !errors.Is(err, redis.ErrAuctionNotFound) {
			status = nethttp.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

type bidRequest struct {
	UserID    string `json:"userId"`
	RequestID string `json:"requestId"`
	Amount    int64  `json:"amount"`
}

func (s *Server) placeBid(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req bidRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	result, err := s.service.PlaceBid(redis.BidCommand{
		AuctionID: r.PathValue("id"),
		UserID:    req.UserID,
		RequestID: req.RequestID,
		Amount:    req.Amount,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		writeJSON(w, nethttp.StatusBadRequest, map[string]any{
			"error":       err.Error(),
			"snapshot":    result.Snapshot,
			"nextMinimum": result.NextMinimum,
		})
		return
	}
	writeJSON(w, nethttp.StatusOK, result)
}

// finishAuction 用于演示或定时任务触发竞拍自然结束。
func (s *Server) finishAuction(w nethttp.ResponseWriter, r *nethttp.Request) {
	snapshot, err := s.service.FinishExpired(r.PathValue("id"), time.Now().UTC())
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, snapshot)
}

// result 返回竞拍最终状态和订单摘要。
func (s *Server) result(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := s.service.GetResult(r.PathValue("id"))
	if err != nil {
		status := nethttp.StatusInternalServerError
		if errors.Is(err, repository.ErrNotFound) {
			status = nethttp.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, result)
}

// events 提供 SSE 兜底事件流，便于浏览器和调试工具直接订阅。
func (s *Server) events(w nethttp.ResponseWriter, r *nethttp.Request) {
	auctionID := r.PathValue("id")
	userID := r.URL.Query().Get("userId")
	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		writeError(w, nethttp.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, cancel := s.service.Subscribe(auctionID)
	defer cancel()
	if snapshot, err := s.service.Snapshot(auctionID, userID); err == nil {
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

// websocketEvents 提供最小 WebSocket 推送，仅负责服务端向客户端发送房间事件。
func (s *Server) websocketEvents(w nethttp.ResponseWriter, r *nethttp.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		writeError(w, nethttp.StatusBadRequest, fmt.Errorf("missing Sec-WebSocket-Key"))
		return
	}
	hijacker, ok := w.(nethttp.Hijacker)
	if !ok {
		writeError(w, nethttp.StatusInternalServerError, fmt.Errorf("websocket hijack unsupported"))
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
	userID := r.URL.Query().Get("userId")
	ch, cancel := s.service.Subscribe(auctionID)
	defer cancel()
	if snapshot, err := s.service.Snapshot(auctionID, userID); err == nil {
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

func writeSSE(w nethttp.ResponseWriter, event string, payload any) {
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "event: %s\n", strings.ReplaceAll(event, "\n", ""))
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

// websocketAccept 计算 WebSocket 握手响应头。
func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// writeWebSocketJSON 将事件编码为未分片文本帧。
func writeWebSocketJSON(conn net.Conn, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeWebSocketText(conn, data)
}

// writeWebSocketText 写入服务端未掩码的 WebSocket 文本帧。
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

// writeJSON 输出 JSON 响应。
func writeJSON(w nethttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError 输出统一错误结构。
func writeError(w nethttp.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// cors 提供本地前后端分端口开发所需的基础跨域支持。
func cors(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == nethttp.MethodOptions {
			w.WriteHeader(nethttp.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
