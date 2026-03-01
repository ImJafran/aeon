package channels

import (
	"bytes"
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

const (
	WhatsAppChannelName = "whatsapp"
	whatsappAPIBase     = "https://graph.facebook.com/v21.0"
	whatsappMaxLen      = 4096
)

// WhatsAppChannel uses Meta's Cloud API for WhatsApp Business messaging.
type WhatsAppChannel struct {
	phoneNumberID string
	accessToken   string
	verifyToken   string
	listenAddr    string
	logger        *slog.Logger
	msgBus        *bus.MessageBus
	server        *http.Server
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	client        *http.Client
}

func NewWhatsApp(phoneNumberID, accessToken, verifyToken, listenAddr string, logger *slog.Logger) *WhatsAppChannel {
	if listenAddr == "" {
		listenAddr = ":8443"
	}
	return &WhatsAppChannel{
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		verifyToken:   verifyToken,
		listenAddr:    listenAddr,
		logger:        logger,
		client:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (wa *WhatsAppChannel) Name() string { return WhatsAppChannelName }

func (wa *WhatsAppChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	wa.msgBus = msgBus
	srvCtx, cancel := context.WithCancel(ctx)
	wa.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", wa.handleWebhook)

	wa.server = &http.Server{
		Addr:    wa.listenAddr,
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return srvCtx
		},
	}

	// Subscribe to outbound messages
	wa.wg.Add(1)
	go func() {
		defer wa.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-srvCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel != WhatsAppChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				wa.sendMessage(msg)
			}
		}
	}()

	// Start HTTP server
	ln, err := net.Listen("tcp", wa.listenAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("whatsapp listen: %w", err)
	}

	wa.wg.Add(1)
	go func() {
		defer wa.wg.Done()
		if err := wa.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			wa.logger.Error("whatsapp server error", "error", err)
		}
	}()

	wa.logger.Info("whatsapp channel started", "addr", wa.listenAddr)
	return nil
}

func (wa *WhatsAppChannel) Stop() {
	if wa.cancel != nil {
		wa.cancel()
	}
	if wa.server != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		wa.server.Shutdown(shutCtx)
	}
	wa.wg.Wait()
}

func (wa *WhatsAppChannel) handleWebhook(rw http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		wa.handleVerification(rw, r)
	case http.MethodPost:
		wa.handleInbound(rw, r)
	default:
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVerification responds to Meta's webhook verification challenge.
func (wa *WhatsAppChannel) handleVerification(rw http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == wa.verifyToken {
		rw.WriteHeader(http.StatusOK)
		rw.Write([]byte(challenge))
		wa.logger.Info("whatsapp webhook verified")
	} else {
		http.Error(rw, "forbidden", http.StatusForbidden)
	}
}

// handleInbound processes incoming WhatsApp messages from Meta's webhook.
func (wa *WhatsAppChannel) handleInbound(rw http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(rw, "bad request", http.StatusBadRequest)
		return
	}

	// Always respond 200 to Meta quickly
	rw.WriteHeader(http.StatusOK)

	var payload waWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		wa.logger.Error("whatsapp parse error", "error", err)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			for _, msg := range change.Value.Messages {
				wa.processInboundMessage(msg)
			}
		}
	}
}

func (wa *WhatsAppChannel) processInboundMessage(msg waMessage) {
	if msg.Type != "text" || msg.Text == nil {
		return
	}

	content := msg.Text.Body
	if content == "" {
		return
	}

	wa.msgBus.Publish(bus.InboundMessage{
		Channel:   WhatsAppChannelName,
		ChatID:    msg.From,
		UserID:    msg.From,
		Content:   content,
		MediaType: bus.MediaText,
		Timestamp: time.Now(),
	})

	wa.logger.Info("whatsapp message received", "from", msg.From)
}

func (wa *WhatsAppChannel) sendMessage(msg bus.OutboundMessage) {
	if msg.ChatID == "" || msg.Content == "" {
		return
	}

	chunks := chunkMessage(msg.Content, whatsappMaxLen)
	for _, chunk := range chunks {
		payload := map[string]interface{}{
			"messaging_product": "whatsapp",
			"to":                msg.ChatID,
			"type":              "text",
			"text": map[string]string{
				"body": chunk,
			},
		}

		data, _ := json.Marshal(payload)
		url := fmt.Sprintf("%s/%s/messages", whatsappAPIBase, wa.phoneNumberID)

		req, err := http.NewRequest("POST", url, bytes.NewReader(data))
		if err != nil {
			wa.logger.Error("whatsapp request build failed", "error", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+wa.accessToken)

		resp, err := wa.client.Do(req)
		if err != nil {
			wa.logger.Error("whatsapp send failed", "error", err, "to", msg.ChatID)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			wa.logger.Error("whatsapp API error", "status", resp.StatusCode, "to", msg.ChatID)
		}
	}
}

// --- WhatsApp Cloud API types ---

type waWebhookPayload struct {
	Entry []waEntry `json:"entry"`
}

type waEntry struct {
	Changes []waChange `json:"changes"`
}

type waChange struct {
	Field string  `json:"field"`
	Value waValue `json:"value"`
}

type waValue struct {
	Messages []waMessage `json:"messages"`
}

type waMessage struct {
	From string  `json:"from"`
	Type string  `json:"type"`
	Text *waText `json:"text,omitempty"`
}

type waText struct {
	Body string `json:"body"`
}
