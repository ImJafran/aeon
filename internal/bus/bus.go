package bus

import (
	"context"
	"sync"
	"time"
)

type MediaType string

const (
	MediaText  MediaType = "text"
	MediaImage MediaType = "image"
	MediaAudio MediaType = "audio"
)

type InboundMessage struct {
	Channel   string
	ChatID    string
	UserID    string
	Content   string
	MediaType MediaType
	MediaURL  string
	Timestamp time.Time
}

type OutboundMessage struct {
	Channel  string
	ChatID   string
	Content  string
	Silent   bool
	Metadata map[string]string
}

type MessageBus struct {
	inbound     chan InboundMessage
	subscribers []chan OutboundMessage
	mu          sync.RWMutex
}

func New(bufferSize int) *MessageBus {
	if bufferSize <= 0 {
		bufferSize = 64
	}
	return &MessageBus{
		inbound: make(chan InboundMessage, bufferSize),
	}
}

func (b *MessageBus) Publish(msg InboundMessage) {
	b.inbound <- msg
}

func (b *MessageBus) Subscribe() chan OutboundMessage {
	ch := make(chan OutboundMessage, 64)
	b.mu.Lock()
	b.subscribers = append(b.subscribers, ch)
	b.mu.Unlock()
	return ch
}

func (b *MessageBus) Send(msg OutboundMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subscribers {
		select {
		case sub <- msg:
		default:
			// drop if subscriber is full
		}
	}
}

func (b *MessageBus) Inbound() <-chan InboundMessage {
	return b.inbound
}

func (b *MessageBus) Close() {
	close(b.inbound)
	b.mu.Lock()
	for _, ch := range b.subscribers {
		close(ch)
	}
	b.mu.Unlock()
}

func (b *MessageBus) Drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-b.inbound:
			if !ok {
				return
			}
		}
	}
}
