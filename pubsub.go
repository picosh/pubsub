package pubsub

import (
	"iter"
	"log/slog"
)

type PubSub interface {
	GetChannels() iter.Seq2[string, *Channel]
	GetClients() iter.Seq2[string, *Client]
	Connect(*Client, []*Channel) (error, error)
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}
