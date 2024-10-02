package pubsub

import (
	"context"
	"io"
	"iter"
	"log/slog"
)

type PubSub interface {
	Connector
	GetPubs() iter.Seq2[string, *Client]
	GetSubs() iter.Seq2[string, *Client]
	GetPipes() iter.Seq2[string, *Client]
	Pipe(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, replay bool) (error, error)
	Sub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error
	Pub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}
