package pubsub

import (
	"io"
	"log/slog"

	"github.com/google/uuid"
)

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
	b.Chan <- sub
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
				log.Info("sub found")
				matches = append(matches, sub)
				writers = append(writers, sub.Writer)
				break
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
		err = b.UnSub(sub)
		if err != nil {
			log.Error("unsub err", "err", err)
		}
	}

	return err
}
