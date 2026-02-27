package channels

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jafran/aeon/internal/bus"
)

const CLIChannelName = "cli"
const CLIChatID = "local"

type CLIChannel struct {
	outbound chan bus.OutboundMessage
	done     chan struct{}
}

func NewCLI() *CLIChannel {
	return &CLIChannel{
		done: make(chan struct{}),
	}
}

func (c *CLIChannel) Name() string {
	return CLIChannelName
}

func (c *CLIChannel) Start(ctx context.Context, b *bus.MessageBus) error {
	c.outbound = b.Subscribe()

	// Read stdin in a goroutine
	go c.readInput(ctx, b)

	// Write outbound messages
	go c.writeOutput(ctx)

	return nil
}

func (c *CLIChannel) Stop() error {
	close(c.done)
	return nil
}

func (c *CLIChannel) readInput(ctx context.Context, b *bus.MessageBus) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		if !scanner.Scan() {
			return
		}

		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("> ")
			continue
		}

		b.Publish(bus.InboundMessage{
			Channel:   CLIChannelName,
			ChatID:    CLIChatID,
			UserID:    "local",
			Content:   text,
			MediaType: bus.MediaText,
			Timestamp: time.Now(),
		})
	}
}

func (c *CLIChannel) writeOutput(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case msg, ok := <-c.outbound:
			if !ok {
				return
			}
			if msg.Channel != CLIChannelName && msg.Channel != "" {
				continue
			}
			if msg.Silent {
				continue
			}
			fmt.Printf("\n%s\n\n> ", msg.Content)
		}
	}
}
