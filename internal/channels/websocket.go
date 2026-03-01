package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/gorilla/websocket"
)

const WebSocketChannelName = "websocket"

type WebSocketChannel struct {
	listenAddr string
	authToken  string
	logger     *slog.Logger
	msgBus     *bus.MessageBus
	server     *http.Server
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	upgrader   websocket.Upgrader

	connsMu sync.RWMutex
	conns   map[string]*wsConn // chatID → connection
}

type wsConn struct {
	conn   *websocket.Conn
	chatID string
	mu     sync.Mutex // protects writes
}

type wsInbound struct {
	Content string `json:"content"`
}

type wsOutbound struct {
	Content string `json:"content"`
}

func NewWebSocket(listenAddr, authToken string, logger *slog.Logger) *WebSocketChannel {
	if listenAddr == "" {
		listenAddr = ":8081"
	}
	return &WebSocketChannel{
		listenAddr: listenAddr,
		authToken:  authToken,
		logger:     logger,
		conns:      make(map[string]*wsConn),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (ws *WebSocketChannel) Name() string { return WebSocketChannelName }

func (ws *WebSocketChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	ws.msgBus = msgBus
	srvCtx, cancel := context.WithCancel(ctx)
	ws.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", ws.handleUpgrade)

	ws.server = &http.Server{
		Addr:    ws.listenAddr,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return srvCtx
		},
	}

	// Subscribe to outbound messages
	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-srvCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel != WebSocketChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				ws.sendToClient(msg.ChatID, msg.Content)
			}
		}
	}()

	// Start HTTP server
	ln, err := net.Listen("tcp", ws.listenAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("websocket listen: %w", err)
	}

	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		if err := ws.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			ws.logger.Error("websocket server error", "error", err)
		}
	}()

	ws.logger.Info("websocket channel started", "addr", ws.listenAddr)
	return nil
}

func (ws *WebSocketChannel) Stop() {
	if ws.cancel != nil {
		ws.cancel()
	}
	// Close all connections
	ws.connsMu.Lock()
	for _, c := range ws.conns {
		c.conn.Close()
	}
	ws.conns = make(map[string]*wsConn)
	ws.connsMu.Unlock()

	if ws.server != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws.server.Shutdown(shutCtx)
	}
	ws.wg.Wait()
}

func (ws *WebSocketChannel) handleUpgrade(rw http.ResponseWriter, r *http.Request) {
	// Auth check
	if ws.authToken != "" {
		token := r.URL.Query().Get("token")
		if token != ws.authToken {
			http.Error(rw, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	chatID := r.URL.Query().Get("chat_id")
	if chatID == "" {
		chatID = fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}

	conn, err := ws.upgrader.Upgrade(rw, r, nil)
	if err != nil {
		ws.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	wc := &wsConn{conn: conn, chatID: chatID}
	ws.connsMu.Lock()
	// Close existing connection for this chatID if any
	if old, ok := ws.conns[chatID]; ok {
		old.conn.Close()
	}
	ws.conns[chatID] = wc
	ws.connsMu.Unlock()

	ws.logger.Info("websocket client connected", "chat_id", chatID)

	// Read loop
	ws.wg.Add(1)
	go func() {
		defer ws.wg.Done()
		defer func() {
			ws.connsMu.Lock()
			if ws.conns[chatID] == wc {
				delete(ws.conns, chatID)
			}
			ws.connsMu.Unlock()
			conn.Close()
			ws.logger.Info("websocket client disconnected", "chat_id", chatID)
		}()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					ws.logger.Error("websocket read error", "error", err, "chat_id", chatID)
				}
				return
			}

			var msg wsInbound
			if err := json.Unmarshal(data, &msg); err != nil {
				ws.logger.Debug("websocket invalid json", "error", err, "chat_id", chatID)
				continue
			}

			if msg.Content == "" {
				continue
			}

			ws.msgBus.Publish(bus.InboundMessage{
				Channel:   WebSocketChannelName,
				ChatID:    chatID,
				UserID:    chatID,
				Content:   msg.Content,
				MediaType: bus.MediaText,
				Timestamp: time.Now(),
			})
		}
	}()
}

func (ws *WebSocketChannel) sendToClient(chatID, content string) {
	ws.connsMu.RLock()
	wc, ok := ws.conns[chatID]
	ws.connsMu.RUnlock()
	if !ok {
		return
	}

	data, _ := json.Marshal(wsOutbound{Content: content})
	wc.mu.Lock()
	err := wc.conn.WriteMessage(websocket.TextMessage, data)
	wc.mu.Unlock()
	if err != nil {
		ws.logger.Error("websocket write error", "error", err, "chat_id", chatID)
	}
}
