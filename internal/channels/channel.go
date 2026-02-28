package channels

import (
	"context"

	"github.com/ImJafran/aeon/internal/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context, b *bus.MessageBus) error
	Stop() error
}
