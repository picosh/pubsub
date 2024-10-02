package pubsub

import (
	"errors"
	"io"
	"iter"
	"log/slog"
	"sync"
	"time"

	"github.com/antoniomika/syncmap"
)

type PubSubMulticast struct {
	Logger   *slog.Logger
	Channels *syncmap.Map[string, *Channel]
}

func (b *PubSubMulticast) Cleanup() {
	toRemove := []string{}
	for _, channel := range b.GetChannels() {
		count := 0

		for range channel.GetClients() {
			count++
		}

		if count == 0 {
			channel.Cleanup()
			toRemove = append(toRemove, channel.ID)
		}
	}

	for _, channel := range toRemove {
		b.Channels.Delete(channel)
	}
}

func (b *PubSubMulticast) GetChannels() iter.Seq2[string, *Channel] {
	return b.Channels.Range
}

func (b *PubSubMulticast) GetClients() iter.Seq2[string, *Client] {
	return func(yield func(string, *Client) bool) {
		for _, channel := range b.GetChannels() {
			channel.Clients.Range(yield)
		}
	}
}

func (b *PubSubMulticast) Connect(client *Client, channels []*Channel) (error, error) {
	for _, channel := range channels {
		dataChannel := b.ensureChannel(channel)
		dataChannel.Clients.Store(client.ID, client)
		defer func() {
			client.Cleanup()
			dataChannel.Clients.Delete(client.ID)

			count := 0
			for _, cl := range dataChannel.GetClients() {
				if cl.Direction == ChannelDirectionInput || cl.Direction == ChannelDirectionInputOutput {
					count++
				}
			}

			if count == 0 {
				for _, cl := range dataChannel.GetClients() {
					cl.Cleanup()
				}
			}

			b.Cleanup()
		}()
		client.Channels.Store(dataChannel.ID, dataChannel)
	}

	var (
		inputErr  error
		outputErr error
		wg        sync.WaitGroup
	)

	// Pub
	if client.Direction == ChannelDirectionInput || client.Direction == ChannelDirectionInputOutput {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				data := make([]byte, 32*1024)
				n, err := client.ReadWriter.Read(data)
				data = data[:n]

				channelMessage := ChannelMessage{
					Data:      data,
					ClientID:  client.ID,
					Direction: ChannelDirectionInput,
				}

				if client.BlockWrite {
					for {
						count := 0
						for _, channel := range client.GetChannels() {
							for _, chanClient := range channel.GetClients() {
								if chanClient.Direction == ChannelDirectionOutput {
									count++
								}
							}
						}

						if count > 0 {
							break
						}

						select {
						case <-time.After(1 * time.Millisecond):
							continue
						case <-client.Done:
							break
						}
					}
				}

				var sendwg sync.WaitGroup

				for _, channel := range client.GetChannels() {
					sendwg.Add(1)
					go func() {
						defer sendwg.Done()
						select {
						case channel.Data <- channelMessage:
						case <-client.Done:
						case <-channel.Done:
						}
					}()
				}

				sendwg.Wait()

				if err != nil {
					if errors.Is(err, io.EOF) {
						return
					}
					inputErr = err
					return
				}
			}
		}()
	}

	// Sub
	if client.Direction == ChannelDirectionOutput || client.Direction == ChannelDirectionInputOutput {
		wg.Add(1)
		go func() {
			defer wg.Done()
		mainLoop:
			for {
				select {
				case data, ok := <-client.Data:
					_, err := client.ReadWriter.Write(data.Data)
					if err != nil {
						outputErr = err
						break mainLoop
					}

					if !ok {
						break mainLoop
					}
				case <-client.Done:
					break mainLoop
				}
			}
		}()
	}

	wg.Wait()

	return inputErr, outputErr
}

func (b *PubSubMulticast) ensureChannel(channel *Channel) *Channel {
	dataChannel, _ := b.Channels.LoadOrStore(channel.ID, channel)
	dataChannel.Handle()
	return dataChannel
}

var _ PubSub = &PubSubMulticast{}
var _ PubSub = (*PubSubMulticast)(nil)
