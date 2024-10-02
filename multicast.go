package pubsub

import (
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
)

type PubSubMulticast struct {
	Connector
	Logger *slog.Logger
}

func (p *PubSubMulticast) getClients(direction ChannelDirection) iter.Seq2[string, *Client] {
	return func(yield func(string, *Client) bool) {
		for clientID, client := range p.GetClients() {
			if client.Direction == direction {
				yield(clientID, client)
			}
		}
	}
}

func (p *PubSubMulticast) GetPipes() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionInputOutput)
}

func (p *PubSubMulticast) GetPubs() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionInput)
}

func (p *PubSubMulticast) GetSubs() iter.Seq2[string, *Client] {
	return p.getClients(ChannelDirectionOutput)
}

func (p *PubSubMulticast) connect(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, direction ChannelDirection, blockWrite bool, replay bool) (error, error) {
	client := NewClient(ID, rw, direction, blockWrite, replay)

	go func() {
		<-ctx.Done()
		client.Cleanup()
	}()

	return p.Connect(client, channels)
}

func (p *PubSubMulticast) Pipe(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel, replay bool) (error, error) {
	return p.connect(ctx, ID, rw, channels, ChannelDirectionInputOutput, false, replay)
}

func (p *PubSubMulticast) Pub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error {
	return errors.Join(p.connect(ctx, ID, rw, channels, ChannelDirectionInput, true, false))
}

func (p *PubSubMulticast) Sub(ctx context.Context, ID string, rw io.ReadWriter, channels []*Channel) error {
	return errors.Join(p.connect(ctx, ID, rw, channels, ChannelDirectionOutput, false, false))
}

var _ PubSub = (*PubSubMulticast)(nil)
