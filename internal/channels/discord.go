package channels

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/bwmarrin/discordgo"
)

const (
	DiscordChannelName = "discord"
	discordMaxLen      = 2000
)

// DiscordChannel connects to Discord via WebSocket using discordgo.
type DiscordChannel struct {
	botToken     string
	allowedUsers map[string]bool
	mentionOnly  bool
	logger       *slog.Logger
	msgBus       *bus.MessageBus
	session      *discordgo.Session
	botUserID    string
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewDiscord(botToken string, allowedUsers []string, mentionOnly bool, logger *slog.Logger) *DiscordChannel {
	allowed := make(map[string]bool, len(allowedUsers))
	for _, u := range allowedUsers {
		allowed[u] = true
	}
	return &DiscordChannel{
		botToken:     botToken,
		allowedUsers: allowed,
		mentionOnly:  mentionOnly,
		logger:       logger,
	}
}

func (d *DiscordChannel) Name() string { return DiscordChannelName }

func (d *DiscordChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	d.msgBus = msgBus
	_, cancel := context.WithCancel(ctx)
	d.cancel = cancel

	session, err := discordgo.New("Bot " + d.botToken)
	if err != nil {
		cancel()
		return err
	}
	d.session = session

	session.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent

	session.AddHandler(d.handleMessage)

	if err := session.Open(); err != nil {
		cancel()
		return err
	}

	d.botUserID = session.State.User.ID

	// Subscribe to outbound messages
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-sub:
				if msg.Channel != DiscordChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				d.sendResponse(msg)
			}
		}
	}()

	d.logger.Info("discord channel started", "user", session.State.User.Username)
	return nil
}

func (d *DiscordChannel) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.session != nil {
		d.session.Close()
	}
	d.wg.Wait()
}

func (d *DiscordChannel) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore own messages
	if m.Author.ID == d.botUserID {
		return
	}

	// Check allowed users
	if len(d.allowedUsers) > 0 && !d.allowedUsers[m.Author.ID] && !d.allowedUsers[m.Author.Username] {
		return
	}

	content := m.Content

	// Mention-only mode: only respond if bot is mentioned
	if d.mentionOnly {
		mentioned := false
		for _, mention := range m.Mentions {
			if mention.ID == d.botUserID {
				mentioned = true
				break
			}
		}
		// Also respond to DMs
		ch, err := s.Channel(m.ChannelID)
		isDM := err == nil && ch.Type == discordgo.ChannelTypeDM
		if !mentioned && !isDM {
			return
		}
		// Strip the mention from content
		content = strings.ReplaceAll(content, "<@"+d.botUserID+">", "")
		content = strings.ReplaceAll(content, "<@!"+d.botUserID+">", "")
		content = strings.TrimSpace(content)
	}

	if content == "" {
		return
	}

	// Show typing indicator
	d.session.ChannelTyping(m.ChannelID)

	d.msgBus.Publish(bus.InboundMessage{
		Channel:   DiscordChannelName,
		ChatID:    m.ChannelID,
		UserID:    m.Author.ID,
		Content:   content,
		MediaType: bus.MediaText,
		Timestamp: time.Now(),
	})
}

func (d *DiscordChannel) sendResponse(msg bus.OutboundMessage) {
	if msg.ChatID == "" || msg.Content == "" {
		return
	}

	chunks := chunkMessage(msg.Content, discordMaxLen)
	for _, chunk := range chunks {
		if _, err := d.session.ChannelMessageSend(msg.ChatID, chunk); err != nil {
			d.logger.Error("discord send failed", "error", err, "chat_id", msg.ChatID)
		}
	}
}
