package pubsub

import (
	"io"
	"log/slog"
	"time"
)

type Subscriber struct {
	ID     string
	Name   string
	Chan   chan error
	Writer io.Writer
}

func (s *Subscriber) Wait() error {
	err := <-s.Chan
	return err
}

type Msg struct {
	Name   string
	Reader io.Reader
	SentAt *time.Time
}

type PubSub interface {
	GetSubs() []*Subscriber
	Sub(sub *Subscriber) error
	UnSub(sub *Subscriber) error
	Pub(msg *Msg) error
	UnPub(msg *Msg) error
	// return true if message should be sent to this subscriber
	PubMatcher(msg *Msg, sub *Subscriber) bool
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}
