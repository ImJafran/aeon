package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
)

const WebhookChannelName = "webhook"

// WebhookChannel exposes an HTTP API for sending messages to Aeon.
// POST /message with JSON body → synchronous response.
type WebhookChannel struct {
	listenAddr string
	authToken  string
	logger     *slog.Logger
	msgBus     *bus.MessageBus
	server     *http.Server
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	// pending tracks in-flight requests waiting for agent responses.
	pending   sync.Map // chatID → chan string
}

type webhookRequest struct {
	ChatID  string `json:"chat_id"`
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

type webhookResponse struct {
	ChatID  string `json:"chat_id"`
	Content string `json:"content"`
}

func NewWebhook(listenAddr, authToken string, logger *slog.Logger) *WebhookChannel {
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	return &WebhookChannel{
		listenAddr: listenAddr,
		authToken:  authToken,
		logger:     logger,
	}
}

func (w *WebhookChannel) Name() string { return WebhookChannelName }

func (w *WebhookChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	w.msgBus = msgBus
	srvCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/message", w.handleMessage)
	mux.HandleFunc("/health", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(`{"status":"ok"}`))
	})

	w.server = &http.Server{
		Addr:    w.listenAddr,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return srvCtx
		},
	}

	// Subscribe to outbound messages and route to pending requests
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-srvCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel != WebhookChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				if ch, ok := w.pending.Load(msg.ChatID); ok {
					select {
					case ch.(chan string) <- msg.Content:
					default:
					}
				}
			}
		}
	}()

	// Start HTTP server
	ln, err := net.Listen("tcp", w.listenAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("webhook listen: %w", err)
	}

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			w.logger.Error("webhook server error", "error", err)
		}
	}()

	w.logger.Info("webhook channel started", "addr", w.listenAddr)
	return nil
}

func (w *WebhookChannel) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.server != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		w.server.Shutdown(shutCtx)
	}
	w.wg.Wait()
}

func (w *WebhookChannel) handleMessage(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Auth check
	if w.authToken != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+w.authToken {
			http.Error(rw, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		http.Error(rw, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	var req webhookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(rw, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(rw, `{"error":"content required"}`, http.StatusBadRequest)
		return
	}
	if req.ChatID == "" {
		req.ChatID = "webhook"
	}
	if req.UserID == "" {
		req.UserID = "webhook"
	}

	// Create response channel and register it
	respCh := make(chan string, 1)
	w.pending.Store(req.ChatID, respCh)
	defer w.pending.Delete(req.ChatID)

	// Publish inbound message
	w.msgBus.Publish(bus.InboundMessage{
		Channel:   WebhookChannelName,
		ChatID:    req.ChatID,
		UserID:    req.UserID,
		Content:   req.Content,
		MediaType: bus.MediaText,
		Timestamp: time.Now(),
	})

	// Wait for response with timeout
	select {
	case content := <-respCh:
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(webhookResponse{
			ChatID:  req.ChatID,
			Content: content,
		})
	case <-time.After(120 * time.Second):
		http.Error(rw, `{"error":"timeout"}`, http.StatusGatewayTimeout)
	case <-r.Context().Done():
		return
	}
}
