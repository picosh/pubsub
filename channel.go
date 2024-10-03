package pubsub

import (
	"iter"
	"sync"

	"github.com/antoniomika/syncmap"
)

type ChannelDirection int

func (d ChannelDirection) String() string {
	return [...]string{"input", "output", "inputoutput"}[d]
}

const (
	ChannelDirectionInput ChannelDirection = iota
	ChannelDirectionOutput
	ChannelDirectionInputOutput
)

type ChannelAction int

func (d ChannelAction) String() string {
	return [...]string{"data", "close"}[d]
}

const (
	ChannelActionData = iota
	ChannelActionClose
)

type ChannelMessage struct {
	Data      []byte
	ClientID  string
	Direction ChannelDirection
	Action    ChannelAction
}

func NewChannel(topic string) *Channel {
	return &Channel{
		Topic:   topic,
		Done:    make(chan struct{}),
		Data:    make(chan ChannelMessage),
		Clients: syncmap.New[string, *Client](),
	}
}

type Channel struct {
	Topic       string
	Done        chan struct{}
	Data        chan ChannelMessage
	Clients     *syncmap.Map[string, *Client]
	handleOnce  sync.Once
	cleanupOnce sync.Once
}

func (c *Channel) GetClients() iter.Seq2[string, *Client] {
	return c.Clients.Range
}

func (c *Channel) Cleanup() {
	c.cleanupOnce.Do(func() {
		close(c.Done)
	})
}

func (c *Channel) Handle() {
	c.handleOnce.Do(func() {
		go func() {
			defer func() {
				for _, client := range c.GetClients() {
					client.Cleanup()
				}
			}()

			for {
				select {
				case <-c.Done:
					return
				case data, ok := <-c.Data:
					var wg sync.WaitGroup
					for _, client := range c.GetClients() {
						if client.Direction == ChannelDirectionInput || (client.ID == data.ClientID && !client.Replay) {
							continue
						}

						wg.Add(1)
						go func() {
							defer wg.Done()
							if !ok {
								client.onceData.Do(func() {
									close(client.Data)
								})
								return
							}

							select {
							case client.Data <- data:
							case <-client.Done:
							case <-c.Done:
							}
						}()
					}
					wg.Wait()
				}
			}
		}()
	})
}
