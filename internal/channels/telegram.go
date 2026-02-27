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

// Transcriber converts audio to text.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte, mimeType string) (string, error)
}

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

	typingMu     sync.Mutex
	typingCancel map[string]context.CancelFunc // per-chat typing indicator cancellers
	typingMsgID  map[string]int                // per-chat "Thinking..." message ID

	transcriber Transcriber
}

// SetTranscriber sets the speech-to-text provider for voice messages.
func (t *TelegramChannel) SetTranscriber(tr Transcriber) {
	t.transcriber = tr
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
		typingCancel: make(map[string]context.CancelFunc),
		typingMsgID:  make(map[string]int),
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
		t.logger.Info("voice message received", "chat_id", msg.Chat.ID, "duration", msg.Voice.Duration)

		if t.transcriber != nil {
			if text, err := t.transcribeVoice(msg.Voice.FileID); err == nil && text != "" {
				content = text
			} else {
				t.logger.Error("voice transcription failed", "error", err)
				content = fmt.Sprintf("[voice message: %ds, transcription failed]", msg.Voice.Duration)
			}
		} else {
			content = fmt.Sprintf("[voice message: %ds, no transcriber configured]", msg.Voice.Duration)
		}
	} else if msg.Audio != nil {
		// Audio file (music, podcast, etc.) — try to transcribe
		t.logger.Info("audio message received", "chat_id", msg.Chat.ID, "duration", msg.Audio.Duration)
		if t.transcriber != nil {
			if text, err := t.transcribeVoice(msg.Audio.FileID); err == nil && text != "" {
				content = text
			} else {
				t.logger.Error("audio transcription failed", "error", err)
				content = fmt.Sprintf("[audio: %ds, title=%s, transcription failed]", msg.Audio.Duration, msg.Audio.Title)
			}
		} else {
			content = fmt.Sprintf("[audio: %ds, title=%s, no transcriber]", msg.Audio.Duration, msg.Audio.Title)
		}
	} else if msg.Video != nil {
		mediaType = bus.MediaImage
		content = fmt.Sprintf("[video: %ds, %dx%d, file_id=%s]", msg.Video.Duration, msg.Video.Width, msg.Video.Height, msg.Video.FileID)
		if msg.Caption != "" {
			content += " " + msg.Caption
		}
	} else if msg.VideoNote != nil {
		content = fmt.Sprintf("[video_note: %ds, file_id=%s]", msg.VideoNote.Duration, msg.VideoNote.FileID)
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

	chatID := fmt.Sprintf("%d", msg.Chat.ID)

	// Publish to bus
	t.msgBus.Publish(bus.InboundMessage{
		Channel:   "telegram",
		ChatID:    chatID,
		UserID:    fmt.Sprintf("%d", msg.From.ID),
		Content:   content,
		MediaType: mediaType,
		Timestamp: time.Unix(int64(msg.Date), 0),
	})

	// Show typing indicator while the agent processes
	t.startTyping(chatID)
}

// handleCallback processes inline keyboard button presses.
func (t *TelegramChannel) handleCallback(cb *tgCallbackQuery) {
	t.logger.Info("callback received", "data", cb.Data, "user", cb.From.ID)

	// Answer the callback to remove the loading indicator
	t.answerCallback(cb.ID)

	chatID := fmt.Sprintf("%d", cb.Message.Chat.ID)

	// Publish as inbound message
	t.msgBus.Publish(bus.InboundMessage{
		Channel: "telegram",
		ChatID:  chatID,
		UserID:  fmt.Sprintf("%d", cb.From.ID),
		Content: cb.Data,
	})

	// Show typing indicator while the agent processes
	t.startTyping(chatID)
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

	// Stop typing indicator now that we have a response
	t.stopTyping(msg.ChatID)

	// Chunk messages that exceed Telegram's limit
	chunks := chunkMessage(content, maxMessageLen)
	for _, chunk := range chunks {
		if err := t.sendMessage(msg.ChatID, chunk); err != nil {
			t.logger.Error("failed to send telegram message", "error", err, "chat_id", msg.ChatID)
		}
	}
}

// --- Typing indicator ---

// sendChatAction sends a chat action (e.g. "typing") to a Telegram chat.
func (t *TelegramChannel) sendChatAction(chatID, action string) {
	body, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"action":  action,
	})
	resp, err := t.client.Post(t.apiURL("sendChatAction"), "application/json", bytes.NewReader(body))
	if err != nil {
		t.logger.Debug("sendChatAction failed", "error", err, "chat_id", chatID)
		return
	}
	resp.Body.Close()
}

// startTyping sends a "Thinking..." placeholder and pulses the typing indicator.
func (t *TelegramChannel) startTyping(chatID string) {
	t.typingMu.Lock()
	// Cancel any existing typing goroutine for this chat
	if cancel, ok := t.typingCancel[chatID]; ok {
		cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.typingCancel[chatID] = cancel
	t.typingMu.Unlock()

	// Send the placeholder message
	if msgID, err := t.sendAndGetID(chatID, "_Thinking..._"); err == nil {
		t.typingMu.Lock()
		t.typingMsgID[chatID] = msgID
		t.typingMu.Unlock()
	}

	go func() {
		t.sendChatAction(chatID, "typing")
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.sendChatAction(chatID, "typing")
			}
		}
	}()
}

// stopTyping cancels the typing goroutine and deletes the "Thinking..." message.
func (t *TelegramChannel) stopTyping(chatID string) {
	t.typingMu.Lock()
	if cancel, ok := t.typingCancel[chatID]; ok {
		cancel()
		delete(t.typingCancel, chatID)
	}
	msgID := t.typingMsgID[chatID]
	delete(t.typingMsgID, chatID)
	t.typingMu.Unlock()

	if msgID != 0 {
		t.deleteMessage(chatID, msgID)
	}
}

// sendAndGetID sends a message and returns its message ID.
func (t *TelegramChannel) sendAndGetID(chatID, text string) (int, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})

	resp, err := t.client.Post(t.apiURL("sendMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result tgResponse[tgMessage]
	if err := json.Unmarshal(data, &result); err != nil {
		return 0, err
	}
	if !result.OK {
		return 0, fmt.Errorf("sendMessage failed: %s", result.Description)
	}
	return result.Result.MessageID, nil
}

// deleteMessage deletes a message by chat and message ID.
func (t *TelegramChannel) deleteMessage(chatID string, messageID int) {
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	})
	resp, err := t.client.Post(t.apiURL("deleteMessage"), "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	resp.Body.Close()
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
	MessageID int          `json:"message_id"`
	From      *tgUser      `json:"from"`
	Chat      tgChat       `json:"chat"`
	Date      int          `json:"date"`
	Text      string       `json:"text"`
	Caption   string       `json:"caption"`
	Voice     *tgVoice     `json:"voice"`
	Audio     *tgAudio     `json:"audio"`
	Video     *tgVideo     `json:"video"`
	VideoNote *tgVideoNote `json:"video_note"`
	Photo     []tgPhoto    `json:"photo"`
	Document  *tgDocument  `json:"document"`
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

type tgAudio struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	Title    string `json:"title"`
}

type tgVideo struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type tgVideoNote struct {
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

// --- Voice transcription ---

// transcribeVoice downloads a Telegram voice file and transcribes it.
func (t *TelegramChannel) transcribeVoice(fileID string) (string, error) {
	fileURL, err := t.GetFileURL(fileID)
	if err != nil {
		return "", fmt.Errorf("getting file URL: %w", err)
	}

	resp, err := t.client.Get(fileURL)
	if err != nil {
		return "", fmt.Errorf("downloading voice: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading voice data: %w", err)
	}

	return t.transcriber.Transcribe(context.Background(), data, "audio/ogg")
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
