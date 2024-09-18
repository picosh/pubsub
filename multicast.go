package pubsub

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/antoniomika/syncmap"
)

/*
multicast:

	every pub event will be sent to all subs on a channel

bidirectional blocking:

	both pub and sub will wait for at least one
	message on a channel before completing
*/
type PubSubMulticast struct {
	Logger   *slog.Logger
	Channels *syncmap.Map[string, *Channel]
}

func (b *PubSubMulticast) ensure(channel string) *Channel {
	dataChannel, _ := b.Channels.LoadOrStore(channel, &Channel{
		Name: channel,
		Done: make(chan struct{}),
		Data: make(chan []byte),
		Chan: make(chan *Sub),
		Subs: syncmap.New[string, *Sub](),
		Pubs: syncmap.New[string, *Pub](),
	})

	return dataChannel
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

func (b *PubSubMulticast) Sub(channel string, sub *Sub) error {
	channelData := b.ensure(channel)

	b.Logger.Info("sub", "channel", channel, "id", sub.ID)

	channelData.Subs.Store(sub.ID, sub)
	defer channelData.Subs.Delete(sub.ID)

	select {
	case channelData.Chan <- sub:
		// message sent
	case <-time.After(10 * time.Millisecond):
		// message dropped
	case <-sub.Done:
		return nil
	}

	return sub.Wait()
}

func (b *PubSubMulticast) Pub(channel string, pub *Pub) error {
	channelData := b.ensure(channel)
	log := b.Logger.With("channel", channel)
	log.Info("pub")

	matches := []*Sub{}
	writers := []io.Writer{}

	channelData.Pubs.Store(pub.ID, pub)
	defer channelData.Pubs.Delete(pub.ID)

	channelData.Subs.Range(func(I string, sub *Sub) bool {
		log.Info("found match", "sub", sub.ID)
		matches = append(matches, sub)
		writers = append(writers, sub.Writer)
		return true
	})

	if len(matches) == 0 {
		for sub := range channelData.Chan {
			log.Info("no subs found, waiting for sub")

			if sub.Writer == nil {
				return fmt.Errorf("pub closed")
			}
			return b.Pub(channel, pub)
		}
	}

	log.Info("copying data")
	del := time.Now()
	pub.SentAt = &del

	writer := io.MultiWriter(writers...)
	_, err := io.Copy(writer, pub.Reader)
	if err != nil {
		log.Error("pub", "err", err)
	}
	for _, sub := range matches {
		select {
		case sub.Error <- err:
		case <-time.After(10 * time.Millisecond):
			continue
		case <-pub.Done:
			return nil
		}
	}

	<-pub.Done

	return err
}

var (
	_ PubSub = &PubSubMulticast{}
)
