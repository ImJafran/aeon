package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jafran/aeon/internal/bus"
)

const (
	telegramAPIBase  = "https://api.telegram.org/bot"
	maxMessageLen    = 4096
	pollTimeout      = 30
)

// TelegramChannel implements the Channel interface for Telegram Bot API.
type TelegramChannel struct {
	token      string
	allowedIDs []int64 // allowed user/chat IDs (empty = allow all)
	msgBus     *bus.MessageBus
	logger     *slog.Logger
	client     *http.Client
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// NewTelegram creates a new Telegram channel.
func NewTelegram(token string, allowedIDs []int64, logger *slog.Logger) *TelegramChannel {
	return &TelegramChannel{
		token:      token,
		allowedIDs: allowedIDs,
		logger:     logger,
		client: &http.Client{
			Timeout: time.Duration(pollTimeout+10) * time.Second,
		},
	}
}

func (t *TelegramChannel) Name() string { return "telegram" }

// Start begins polling and subscribes to outbound messages.
func (t *TelegramChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	t.msgBus = msgBus
	pollCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	// Start polling goroutine
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.pollLoop(pollCtx)
	}()

	// Subscribe to outbound messages
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-pollCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel == "telegram" || msg.Channel == "" {
					t.sendResponse(msg)
				}
			}
		}
	}()

	t.logger.Info("telegram channel started")
	return nil
}

// Stop shuts down the Telegram channel.
func (t *TelegramChannel) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	t.wg.Wait()
}

// pollLoop runs the Telegram long-polling loop.
func (t *TelegramChannel) pollLoop(ctx context.Context) {
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := t.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.logger.Error("telegram poll error", "error", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			t.handleUpdate(ctx, update)
		}
	}
}

// handleUpdate processes a single Telegram update.
func (t *TelegramChannel) handleUpdate(_ context.Context, update tgUpdate) {
	msg := update.Message
	if msg == nil {
		// Could be a callback query (button press)
		if update.CallbackQuery != nil {
			t.handleCallback(update.CallbackQuery)
		}
		return
	}

	// Check allowed users
	if len(t.allowedIDs) > 0 {
		allowed := false
		for _, id := range t.allowedIDs {
			if msg.Chat.ID == id || msg.From.ID == id {
				allowed = true
				break
			}
		}
		if !allowed {
			t.logger.Warn("unauthorized telegram user", "user_id", msg.From.ID, "chat_id", msg.Chat.ID)
			return
		}
	}

	// Determine content type
	content := msg.Text
	mediaType := bus.MediaText

	if msg.Voice != nil {
		mediaType = bus.MediaAudio
		content = fmt.Sprintf("[voice message: file_id=%s, duration=%ds]", msg.Voice.FileID, msg.Voice.Duration)
	} else if msg.Photo != nil && len(msg.Photo) > 0 {
		mediaType = bus.MediaImage
		// Get the largest photo
		largest := msg.Photo[len(msg.Photo)-1]
		content = fmt.Sprintf("[image: file_id=%s]", largest.FileID)
		if msg.Caption != "" {
			content += " " + msg.Caption
		}
	} else if msg.Document != nil {
		mediaType = bus.MediaText
		content = fmt.Sprintf("[document: %s, file_id=%s]", msg.Document.FileName, msg.Document.FileID)
	}

	if content == "" {
		return
	}

	// Publish to bus
	t.msgBus.Publish(bus.InboundMessage{
		Channel:   "telegram",
		ChatID:    fmt.Sprintf("%d", msg.Chat.ID),
		UserID:    fmt.Sprintf("%d", msg.From.ID),
		Content:   content,
		MediaType: mediaType,
		Timestamp: time.Unix(int64(msg.Date), 0),
	})
}

// handleCallback processes inline keyboard button presses.
func (t *TelegramChannel) handleCallback(cb *tgCallbackQuery) {
	t.logger.Info("callback received", "data", cb.Data, "user", cb.From.ID)

	// Answer the callback to remove the loading indicator
	t.answerCallback(cb.ID)

	// Publish as inbound message
	t.msgBus.Publish(bus.InboundMessage{
		Channel: "telegram",
		ChatID:  fmt.Sprintf("%d", cb.Message.Chat.ID),
		UserID:  fmt.Sprintf("%d", cb.From.ID),
		Content: cb.Data,
	})
}

// sendResponse sends an outbound message to Telegram.
func (t *TelegramChannel) sendResponse(msg bus.OutboundMessage) {
	if msg.ChatID == "" {
		return
	}

	content := msg.Content
	if content == "" {
		return
	}

	// Chunk messages that exceed Telegram's limit
	chunks := chunkMessage(content, maxMessageLen)
	for _, chunk := range chunks {
		if err := t.sendMessage(msg.ChatID, chunk); err != nil {
			t.logger.Error("failed to send telegram message", "error", err, "chat_id", msg.ChatID)
		}
	}
}

// --- Telegram API methods ---

func (t *TelegramChannel) apiURL(method string) string {
	return telegramAPIBase + t.token + "/" + method
}

func (t *TelegramChannel) getUpdates(ctx context.Context, offset int) ([]tgUpdate, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"offset":  offset,
		"timeout": pollTimeout,
		"allowed_updates": []string{"message", "callback_query"},
	})

	req, err := http.NewRequestWithContext(ctx, "POST", t.apiURL("getUpdates"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result tgResponse[[]tgUpdate]
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error: %s", result.Description)
	}

	return result.Result, nil
}

func (t *TelegramChannel) sendMessage(chatID, text string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	resp, err := t.client.Post(t.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		// If Markdown fails, retry without parse_mode
		if strings.Contains(string(data), "can't parse entities") {
			return t.sendMessagePlain(chatID, text)
		}
		return fmt.Errorf("sendMessage failed: %s", string(data))
	}

	return nil
}

func (t *TelegramChannel) sendMessagePlain(chatID, text string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	})

	resp, err := t.client.Post(t.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sendMessage (plain) failed: %s", string(data))
	}
	return nil
}

// SendWithKeyboard sends a message with inline keyboard buttons.
func (t *TelegramChannel) SendWithKeyboard(chatID, text string, buttons [][]InlineButton) error {
	keyboard := make([][]map[string]string, len(buttons))
	for i, row := range buttons {
		keyboard[i] = make([]map[string]string, len(row))
		for j, btn := range row {
			keyboard[i][j] = map[string]string{
				"text":          btn.Text,
				"callback_data": btn.Data,
			}
		}
	}

	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":      chatID,
		"text":         text,
		"reply_markup": map[string]interface{}{"inline_keyboard": keyboard},
	})

	resp, err := t.client.Post(t.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sendMessage with keyboard failed: %s", string(data))
	}
	return nil
}

func (t *TelegramChannel) answerCallback(callbackID string) {
	body, _ := json.Marshal(map[string]string{
		"callback_query_id": callbackID,
	})
	resp, err := t.client.Post(t.apiURL("answerCallbackQuery"), "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
}

// GetFileURL retrieves the download URL for a Telegram file.
func (t *TelegramChannel) GetFileURL(fileID string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"file_id": fileID,
	})

	resp, err := t.client.Post(t.apiURL("getFile"), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result tgResponse[tgFile]
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	if !result.OK {
		return "", fmt.Errorf("getFile failed: %s", result.Description)
	}

	return fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", t.token, result.Result.FilePath), nil
}

// InlineButton represents an inline keyboard button.
type InlineButton struct {
	Text string
	Data string
}

// --- Telegram API types ---

type tgResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

type tgUpdate struct {
	UpdateID      int              `json:"update_id"`
	Message       *tgMessage       `json:"message"`
	CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgMessage struct {
	MessageID int         `json:"message_id"`
	From      *tgUser     `json:"from"`
	Chat      tgChat      `json:"chat"`
	Date      int         `json:"date"`
	Text      string      `json:"text"`
	Caption   string      `json:"caption"`
	Voice     *tgVoice    `json:"voice"`
	Photo     []tgPhoto   `json:"photo"`
	Document  *tgDocument `json:"document"`
}

type tgUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type tgVoice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
}

type tgPhoto struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type tgDocument struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    *tgUser    `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

type tgFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
}

// --- Utilities ---

// chunkMessage splits a message into chunks respecting Telegram's max length.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		// Try to split at last newline before limit
		cut := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cut = idx + 1
		}

		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}

	return chunks
}
