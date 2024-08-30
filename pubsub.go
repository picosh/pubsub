package pubsub

import (
	"io"
	"log/slog"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/google/uuid"
)

type Subscriber struct {
	ID      string
	Name    string
	Session ssh.Session
	Chan    chan error
}

func (s *Subscriber) Wait() error {
	err := <-s.Chan
	return err
}

type Msg struct {
	Name   string
	Reader io.Reader
}

type PubSub interface {
	GetSubs() []*Subscriber
	Sub(l *Subscriber) error
	UnSub(l *Subscriber) error
	Pub(msg *Msg) error
}

type PubSubMulticast struct {
	Logger *slog.Logger
	subs   []*Subscriber
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

func (b *PubSubMulticast) Pub(msg *Msg) error {
	log := b.Logger.With("channel", msg.Name)
	log.Info("pub")

	matches := []*Subscriber{}
	writers := []io.Writer{}
	for _, sub := range b.subs {
		if sub.Name == msg.Name {
			matches = append(matches, sub)
			writers = append(writers, sub.Session)
		}
	}

	if len(matches) == 0 {
		log.Info("no subs found")
	}

	log.Info("copying data")
	writer := io.MultiWriter(writers...)
	_, err := io.Copy(writer, msg.Reader)
	if err != nil {
		log.Error("pub", "err", err)
	}
	for _, sub := range matches {
		sub.Chan <- err
		b.UnSub(sub)
	}

	return err
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}

func PubSubMiddleware(cfg *Cfg) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
			if len(args) < 2 {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub) {channel}")
				next(sesh)
				return
			}

			cmd := strings.TrimSpace(args[0])
			channel := args[1]
			logger := cfg.Logger.With(
				"cmd", cmd,
				"channel", channel,
			)

			logger.Info("running cli")

			if cmd == "help" {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub) {channel}")
			} else if cmd == "sub" {
				sub := &Subscriber{
					Name:    channel,
					Session: sesh,
					Chan:    make(chan error),
				}
				err := cfg.PubSub.Sub(sub)
				if err != nil {
					wish.Errorln(sesh, err)
				}
				/* defer func() {
					err = cfg.PubSub.UnSub(listener)
					if err != nil {
						wish.Errorln(sesh, err)
					}
				}() */
			} else if cmd == "pub" {
				msg := &Msg{
					Name:   channel,
					Reader: sesh,
				}
				err := cfg.PubSub.Pub(msg)
				wish.Errorln(sesh, err)
			} else {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub) {channel}")
			}

			next(sesh)
		}
	}
}
