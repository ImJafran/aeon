package bus

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New(10)
	defer b.Close()

	sub := b.Subscribe()

	// Publish an inbound message
	b.Publish(InboundMessage{
		Channel:   "test",
		ChatID:    "123",
		Content:   "hello",
		MediaType: MediaText,
		Timestamp: time.Now(),
	})

	// Read it from inbound
	select {
	case msg := <-b.Inbound():
		if msg.Content != "hello" {
			t.Errorf("expected 'hello', got '%s'", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout reading inbound")
	}

	// Send an outbound message
	b.Send(OutboundMessage{
		Channel: "test",
		ChatID:  "123",
		Content: "world",
	})

	// Read from subscriber
	select {
	case msg := <-sub:
		if msg.Content != "world" {
			t.Errorf("expected 'world', got '%s'", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout reading subscriber")
	}
}
