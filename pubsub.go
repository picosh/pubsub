package pubsub

import (
	"context"
	"io"
	"iter"
)

type PubSub interface {
	Broker
	GetPubs() iter.Seq2[string, *Client]
	GetSubs() iter.Seq2[string, *Client]
	GetPipes() iter.Seq2[string, *Client]
	Pipe(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, replay bool) (error, error)
	Sub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, keepAlive bool) error
	Pub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error
}
