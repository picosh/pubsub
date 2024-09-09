package pubsub

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

/*
multicast:

	every pub event will be sent to all subs on a channel

bidirectional blocking:

	both pub and sub will wait for at least one
	message on a channel before completing
*/
type PubSubMulticast struct {
	Logger *slog.Logger
	subs   []*Subscriber
	Chan   chan *Subscriber
}

func (b *PubSubMulticast) GetSubs() []*Subscriber {
	b.Logger.Info("getsubs")
	return b.subs
}

func (b *PubSubMulticast) Sub(sub *Subscriber) error {
	id := uuid.New()
	sub.ID = id.String()
	b.Logger.Info("sub", "channel", sub.Name, "id", id)
	b.subs = append(b.subs, sub)
	select {
	case b.Chan <- sub:
		// message sent
	default:
		// message dropped
	}

	return sub.Wait()
}

func (b *PubSubMulticast) UnSub(rm *Subscriber) error {
	b.Logger.Info("unsub", "channel", rm.Name, "id", rm.ID)
	next := []*Subscriber{}
	for _, sub := range b.subs {
		if sub.ID != rm.ID {
			next = append(next, sub)
		}
	}
	b.subs = next
	return nil
}

func (b *PubSubMulticast) PubMatcher(msg *Msg, sub *Subscriber) bool {
	return msg.Name == sub.Name
}

func (b *PubSubMulticast) Pub(msg *Msg) error {
	log := b.Logger.With("channel", msg.Name)
	log.Info("pub")

	matches := []*Subscriber{}
	writers := []io.Writer{}
	for _, sub := range b.subs {
		if b.PubMatcher(msg, sub) {
			log.Info("found match", "sub", sub.ID)
			matches = append(matches, sub)
			writers = append(writers, sub.Writer)
		}
	}

	if len(matches) == 0 {
		var sub *Subscriber
		for {
			log.Info("no subs found, waiting for sub")
			sub = <-b.Chan
			if b.PubMatcher(msg, sub) {
				// empty subscriber is a signal to force a pub to stop
				// waiting for a sub
				if sub.Writer == nil {
					return fmt.Errorf("pub closed")
				}
				return b.Pub(msg)
			}
		}
	}

	log.Info("copying data")
	writer := io.MultiWriter(writers...)
	_, err := io.Copy(writer, msg.Reader)
	if err != nil {
		log.Error("pub", "err", err)
	}
	for _, sub := range matches {
		sub.Chan <- err
		log.Info("sub unsub")
		err = b.UnSub(sub)
		if err != nil {
			log.Error("unsub err", "err", err)
		}
	}
	del := time.Now()
	msg.Delivered = &del

	return err
}

func (b *PubSubMulticast) UnPub(msg *Msg) error {
	b.Logger.Info("unpub", "channel", msg.Name)
	// if the message hasn't been delivered then send a cancel sub to
	// the multicast channel
	if msg.Delivered == nil {
		b.Chan <- &Subscriber{Name: msg.Name}
	}
	return nil
}
