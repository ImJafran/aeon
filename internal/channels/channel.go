package channels

import (
	"context"

	"github.com/jafran/aeon/internal/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context, b *bus.MessageBus) error
	Stop() error
}
