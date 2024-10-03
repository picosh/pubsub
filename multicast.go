package pubsub

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"

	"github.com/antoniomika/syncmap"
)

type Multicast struct {
	Broker
	Logger *slog.Logger
}

func NewMulticast(logger *slog.Logger) *Multicast {
	return &Multicast{
		Logger: logger,
		Broker: &BaseBroker{
			Channels: syncmap.New[string, *Channel](),
		},
	}
}

func (p *Multicast) getClients(direction ChannelDirection) iter.Seq2[string, *Client] {
	return func(yield func(string, *Client) bool) {
		for clientID, client := range p.GetClients() {
			if client.Direction == direction {
				yield(clientID, client)
			}
		}
	}
}

func (p *Multicast) GetPipes() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionInputOutput)
}

func (p *Multicast) GetPubs() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionInput)
}

func (p *Multicast) GetSubs() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionOutput)
}

func (p *Multicast) connect(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, direction ChannelDirection, blockWrite bool, replay, keepAlive bool) (error, error) {
	client := NewClient(ID, rw, direction, blockWrite, replay, keepAlive)

	go func() {
		<-ctx.Done()
		client.Cleanup()
	}()

	return p.Connect(client, channels)
}

func (p *Multicast) Pipe(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, replay bool) (error, error) {
	return p.connect(ctx, ID, rw, channels, ChannelDirectionInputOutput, false, replay, false)
}

func (p *Multicast) Pub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error {
	return errors.Join(p.connect(ctx, ID, rw, channels, ChannelDirectionInput, true, false, false))
}

func (p *Multicast) Sub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, keepAlive bool) error {
	return errors.Join(p.connect(ctx, ID, rw, channels, ChannelDirectionOutput, false, false, keepAlive))
}

var _ = (*Multicast)(nil)
