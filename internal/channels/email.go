package channels

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
)

const EmailChannelName = "email"

// EmailChannel polls an IMAP inbox for new messages and replies via SMTP.
type EmailChannel struct {
	imapServer   string
	smtpServer   string
	username     string
	password     string
	pollInterval time.Duration
	allowedFrom  map[string]bool
	logger       *slog.Logger
	msgBus       *bus.MessageBus
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewEmail(imapServer, smtpServer, username, password string, pollInterval time.Duration, allowedFrom []string, logger *slog.Logger) *EmailChannel {
	if pollInterval <= 0 {
		pollInterval = 60 * time.Second
	}
	allowed := make(map[string]bool, len(allowedFrom))
	for _, addr := range allowedFrom {
		allowed[strings.ToLower(addr)] = true
	}
	return &EmailChannel{
		imapServer:   imapServer,
		smtpServer:   smtpServer,
		username:     username,
		password:     password,
		pollInterval: pollInterval,
		allowedFrom:  allowed,
		logger:       logger,
	}
}

func (e *EmailChannel) Name() string { return EmailChannelName }

func (e *EmailChannel) Start(ctx context.Context, msgBus *bus.MessageBus) error {
	e.msgBus = msgBus
	eCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	// Poll loop
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.pollLoop(eCtx)
	}()

	// Subscribe to outbound messages
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		sub := msgBus.Subscribe()
		for {
			select {
			case <-eCtx.Done():
				return
			case msg := <-sub:
				if msg.Channel != EmailChannelName {
					continue
				}
				if msg.Metadata != nil && msg.Metadata[bus.MetaStatus] == "true" {
					continue
				}
				e.sendReply(msg)
			}
		}
	}()

	e.logger.Info("email channel started", "imap", e.imapServer, "poll", e.pollInterval)
	return nil
}

func (e *EmailChannel) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
}

func (e *EmailChannel) pollLoop(ctx context.Context) {
	// Initial poll
	e.checkInbox()

	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkInbox()
		}
	}
}

func (e *EmailChannel) checkInbox() {
	c, err := imapclient.DialTLS(e.imapServer, nil)
	if err != nil {
		e.logger.Error("imap connect failed", "error", err)
		return
	}
	defer c.Logout()

	if err := c.Login(e.username, e.password); err != nil {
		e.logger.Error("imap login failed", "error", err)
		return
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		e.logger.Error("imap select failed", "error", err)
		return
	}

	if mbox.Messages == 0 {
		return
	}

	// Search for unseen messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{imap.SeenFlag}
	ids, err := c.Search(criteria)
	if err != nil {
		e.logger.Error("imap search failed", "error", err)
		return
	}

	if len(ids) == 0 {
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(ids...)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	for msg := range messages {
		e.processMessage(msg, section)
	}

	if err := <-done; err != nil {
		e.logger.Error("imap fetch failed", "error", err)
	}

	// Mark as seen
	storeItem := imap.FormatFlagsOp(imap.AddFlags, true)
	if err := c.Store(seqset, storeItem, []interface{}{imap.SeenFlag}, nil); err != nil {
		e.logger.Error("imap store flags failed", "error", err)
	}
}

func (e *EmailChannel) processMessage(msg *imap.Message, section *imap.BodySectionName) {
	if msg.Envelope == nil {
		return
	}

	// Get sender
	var from string
	if len(msg.Envelope.From) > 0 {
		addr := msg.Envelope.From[0]
		from = addr.Address()
	}
	if from == "" {
		return
	}

	// Check allowed senders
	if len(e.allowedFrom) > 0 && !e.allowedFrom[strings.ToLower(from)] {
		e.logger.Debug("email from non-allowed sender", "from", from)
		return
	}

	// Extract body
	body := msg.GetBody(section)
	if body == nil {
		return
	}

	mr, err := mail.CreateReader(body)
	if err != nil {
		e.logger.Error("email parse failed", "error", err, "from", from)
		return
	}

	var textContent string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch p.Header.(type) {
		case *mail.InlineHeader:
			b, err := io.ReadAll(p.Body)
			if err != nil {
				continue
			}
			textContent = string(b)
		}
	}

	// Fall back to subject if no body
	content := strings.TrimSpace(textContent)
	if content == "" {
		content = msg.Envelope.Subject
	}
	if content == "" {
		return
	}

	// Prepend subject if we have both
	if textContent != "" && msg.Envelope.Subject != "" {
		content = fmt.Sprintf("[%s] %s", msg.Envelope.Subject, content)
	}

	e.msgBus.Publish(bus.InboundMessage{
		Channel:   EmailChannelName,
		ChatID:    strings.ToLower(from),
		UserID:    strings.ToLower(from),
		Content:   content,
		MediaType: bus.MediaText,
		Timestamp: msg.Envelope.Date,
	})

	e.logger.Info("email received", "from", from, "subject", msg.Envelope.Subject)
}

func (e *EmailChannel) sendReply(msg bus.OutboundMessage) {
	if msg.ChatID == "" || msg.Content == "" {
		return
	}

	to := msg.ChatID // chatID is the sender's email address
	subject := "Re: Aeon"

	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		e.username, to, subject, msg.Content)

	// Parse SMTP server host for auth
	host := e.smtpServer
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}

	auth := smtp.PlainAuth("", e.username, e.password, host)
	if err := smtp.SendMail(e.smtpServer, auth, e.username, []string{to}, []byte(body)); err != nil {
		e.logger.Error("smtp send failed", "error", err, "to", to)
	} else {
		e.logger.Info("email sent", "to", to)
	}
}
