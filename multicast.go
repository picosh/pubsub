package pubsub

import (
	"errors"
	"io"
	"log/slog"
	"strings"

	"github.com/antoniomika/syncmap"
)

type PubSubMulticast struct {
	Logger   *slog.Logger
	Channels *syncmap.Map[string, *Channel]
}

func (b *PubSubMulticast) Cleanup() {
	toRemove := []string{}
	b.Channels.Range(func(I string, J *Channel) bool {
		count := 0
		J.Pubs.Range(func(K string, V *Pub) bool {
			count++
			return true
		})

		J.Subs.Range(func(K string, V *Sub) bool {
			count++
			return true
		})

		if count == 0 {
			J.Cleanup()
			toRemove = append(toRemove, I)
		}

		return true
	})

	for _, channel := range toRemove {
		b.Channels.Delete(channel)
	}
}

func (b *PubSubMulticast) GetChannels(channelPrefix string) []*Channel {
	var chans []*Channel
	b.Channels.Range(func(I string, J *Channel) bool {
		if strings.HasPrefix(I, channelPrefix) {
			chans = append(chans, J)
		}

		return true
	})
	return chans
}

func (b *PubSubMulticast) GetChannel(channel string) *Channel {
	channelData, _ := b.Channels.Load(channel)
	return channelData
}

func (b *PubSubMulticast) GetPubs(channel string) []*Pub {
	var pubs []*Pub
	b.Channels.Range(func(I string, J *Channel) bool {
		found := channel == I
		if found || channel == "*" {
			J.Pubs.Range(func(K string, V *Pub) bool {
				pubs = append(pubs, V)
				return true
			})
		}

		return !found
	})
	return pubs
}

func (b *PubSubMulticast) GetSubs(channel string) []*Sub {
	var subs []*Sub
	b.Channels.Range(func(I string, J *Channel) bool {
		found := channel == I
		if found || channel == "*" {
			J.Subs.Range(func(K string, V *Sub) bool {
				subs = append(subs, V)
				return true
			})
		}

		return !found
	})
	return subs
}

func (b *PubSubMulticast) ensure(channel string) *Channel {
	dataChannel, _ := b.Channels.LoadOrStore(channel, &Channel{
		Name: channel,
		Done: make(chan struct{}),
		Data: make(chan []byte),
		Subs: syncmap.New[string, *Sub](),
		Pubs: syncmap.New[string, *Pub](),
	})
	dataChannel.Handle()

	return dataChannel
}

func (b *PubSubMulticast) Sub(channel string, sub *Sub) error {
	dataChannel := b.ensure(channel)
	dataChannel.Subs.Store(sub.ID, sub)
	defer func() {
		sub.Cleanup()
		dataChannel.Subs.Delete(sub.ID)
		b.Cleanup()
	}()

mainLoop:
	for {
		select {
		case <-sub.Done:
			break mainLoop
		case <-dataChannel.Done:
			break mainLoop
		case data, ok := <-sub.Data:
			_, err := sub.Writer.Write(data)
			if err != nil {
				slog.Error("error writing to sub", slog.Any("sub", sub.ID), slog.Any("channel", channel), slog.Any("error", err))
				return err
			}

			if !ok {
				break mainLoop
			}
		}
	}

	return nil
}

func (b *PubSubMulticast) Pub(channel string, pub *Pub) error {
	dataChannel := b.ensure(channel)
	dataChannel.Pubs.Store(pub.ID, pub)
	defer func() {
		pub.Cleanup()
		dataChannel.Pubs.Delete(pub.ID)

		count := 0
		dataChannel.Pubs.Range(func(I string, J *Pub) bool {
			count++
			return true
		})

		if count == 0 {
			dataChannel.onceData.Do(func() {
				close(dataChannel.Data)
			})
		}

		b.Cleanup()
	}()

mainLoop:
	for {
		select {
		case <-pub.Done:
			break mainLoop
		case <-dataChannel.Done:
			break mainLoop
		default:
			data := make([]byte, 32*1024)
			n, err := pub.Reader.Read(data)
			data = data[:n]

			select {
			case dataChannel.Data <- data:
			case <-pub.Done:
				break mainLoop
			case <-dataChannel.Done:
				break mainLoop
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}

				slog.Error("error reading from pub", slog.Any("pub", pub.ID), slog.Any("channel", channel), slog.Any("error", err))
				return err
			}
		}
	}

	return nil
}
