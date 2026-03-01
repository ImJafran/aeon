package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	SlackChannelName = "slack"
	slackMaxLen      = 4000
)

// SlackChannel connects to Slack via Socket Mode (no public URL needed).
type SlackChannel struct {
	botToken     string
	appToken     string
	allowedUsers map[string]bool
	logger       *slog.Logger
	msgBus       *bus.MessageBus
	client       *slack.Client
	socket       *socketmode.Client
	botUserID    string
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewSlack(botToken, appToken string, allowedUsers []string, logger *slog.Logger) *SlackChannel {
	allowed := make(map[string]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allowed[u] = true
	}
	return &SlackChannel{
		botToken:     botToken,
		appToken:     appToken,
		allowedUsers: allowed,
		logger:       logger,
	}
}

func (s *SlackChannel) Name() string { return SlackChannelName }

func (s *SlackChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	s.msgBus = msgBus
	sCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.client = slack.New(s.botToken, slack.OptionAppLevelToken(s.appToken))

	// Get bot user ID
	authResp, err := s.client.AuthTest()
	if err != nil {
		cancel()
		return fmt.Errorf("slack auth test: %w", err)
	}
	s.botUserID = authResp.UserID

	s.socket = socketmode.New(s.client)

	// Event handler goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleEvents(sCtx)
	}()

	// Socket Mode run goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.socket.RunContext(sCtx); err != nil && sCtx.Err() == nil {
			s.logger.Error("slack socket mode error", "error", err)
		}
	}()

	// Subscribe to outbound messages
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-sCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel != SlackChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				s.sendResponse(sCtx, msg)
			}
		}
	}()

	s.logger.Info("slack channel started", "bot_user", s.botUserID)
	return nil
}

func (s *SlackChannel) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *SlackChannel) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-s.socket.Events:
			if !ok {
				return
			}
			s.processEvent(ctx, evt)
		}
	}
}

func (s *SlackChannel) processEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		evtAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		s.socket.Ack(*evt.Request)
		s.handleEventsAPI(ctx, evtAPI)
	}
}

func (s *SlackChannel) handleEventsAPI(_ context.Context, evt slackevents.EventsAPIEvent) {
	switch evt.Type {
	case slackevents.CallbackEvent:
		inner := evt.InnerEvent
		switch ev := inner.Data.(type) {
		case *slackevents.MessageEvent:
			s.handleMessageEvent(ev)
		case *slackevents.AppMentionEvent:
			s.handleMentionEvent(ev)
		}
	}
}

func (s *SlackChannel) handleMessageEvent(ev *slackevents.MessageEvent) {
	// Ignore bot messages and message changes/deletions
	if ev.BotID != "" || ev.User == s.botUserID || ev.SubType != "" {
		return
	}

	// Check allowed users
	if len(s.allowedUsers) > 0 && !s.allowedUsers[ev.User] {
		return
	}

	content := ev.Text
	if content == "" {
		return
	}

	// Build chat ID: channel or channel/thread
	chatID := ev.Channel
	if ev.ThreadTimeStamp != "" {
		chatID = ev.Channel + "/" + ev.ThreadTimeStamp
	}

	s.msgBus.Publish(bus.InboundMessage{
		Channel:   SlackChannelName,
		ChatID:    chatID,
		UserID:    ev.User,
		Content:   content,
		MediaType: bus.MediaText,
		Timestamp: time.Now(),
	})
}

func (s *SlackChannel) handleMentionEvent(ev *slackevents.AppMentionEvent) {
	if ev.User == s.botUserID {
		return
	}

	if len(s.allowedUsers) > 0 && !s.allowedUsers[ev.User] {
		return
	}

	// Strip bot mention from content
	content := ev.Text
	content = strings.ReplaceAll(content, "<@"+s.botUserID+">", "")
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	chatID := ev.Channel
	if ev.ThreadTimeStamp != "" {
		chatID = ev.Channel + "/" + ev.ThreadTimeStamp
	}

	s.msgBus.Publish(bus.InboundMessage{
		Channel:   SlackChannelName,
		ChatID:    chatID,
		UserID:    ev.User,
		Content:   content,
		MediaType: bus.MediaText,
		Timestamp: time.Now(),
	})
}

func (s *SlackChannel) sendResponse(ctx context.Context, msg bus.OutboundMessage) {
	if msg.ChatID == "" || msg.Content == "" {
		return
	}

	// Parse chatID: "channel" or "channel/threadTS"
	channel := msg.ChatID
	var threadTS string
	if idx := strings.Index(msg.ChatID, "/"); idx > 0 {
		channel = msg.ChatID[:idx]
		threadTS = msg.ChatID[idx+1:]
	}

	chunks := chunkMessage(msg.Content, slackMaxLen)
	for _, chunk := range chunks {
		opts := []slack.MsgOption{
			slack.MsgOptionText(chunk, false),
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		if _, _, err := s.client.PostMessageContext(ctx, channel, opts...); err != nil {
			s.logger.Error("slack send failed", "error", err, "channel", channel)
		}
	}
}
