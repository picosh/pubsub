package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/google/uuid"
)

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

type Subscriber struct {
	ID      string
	Channel string
	Session ssh.Session
}
type Msg struct {
	Channel string
	Reader  io.Reader
}

type PubSub interface {
	GetSubs() []*Subscriber
	Sub(l *Subscriber) error
	UnSub(l *Subscriber) error
	Pub(msg *Msg) []error
}

type BasicPubSub struct {
	logger *slog.Logger
	subs   []*Subscriber
}

func (b *BasicPubSub) GetSubs() []*Subscriber {
	b.logger.Info("getsubs")
	return b.subs
}

func (b *BasicPubSub) Sub(sub *Subscriber) error {
	b.logger.Info("sub", "channel", sub.Channel)
	id := uuid.New()
	sub.ID = id.String()
	b.subs = append(b.subs, sub)
	return nil
}

func (b *BasicPubSub) UnSub(rm *Subscriber) error {
	b.logger.Info("unsub", "channel", rm.Channel)
	next := []*Subscriber{}
	for _, sub := range b.subs {
		if sub.ID != rm.ID {
			next = append(next, sub)
		}
	}
	b.subs = next
	return nil
}

func (b *BasicPubSub) Pub(msg *Msg) []error {
	b.logger.Info("sub", "channel", msg.Channel)
	errs := []error{}
	for _, sub := range b.subs {
		if sub.Channel != msg.Channel {
			continue
		}

		_, err := io.Copy(sub.Session, msg.Reader)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}

func PubSubMiddleware(cfg *Cfg) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
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
				listener := &Subscriber{
					Channel: channel,
					Session: sesh,
				}
				err := cfg.PubSub.Sub(listener)
				if err != nil {
					wish.Errorln(sesh, err)
				}
				defer func() {
					err = cfg.PubSub.UnSub(listener)
					if err != nil {
						wish.Errorln(sesh, err)
					}
				}()
			} else if cmd == "pub" {
				msg := &Msg{
					Channel: channel,
					Reader:  sesh,
				}
				errs := cfg.PubSub.Pub(msg)
				if errs != nil {
					for _, err := range errs {
						wish.Errorln(sesh, err)
					}
				}
			}

			next(sesh)
		}
	}
}

func main() {
	logger := slog.Default()
	host := GetEnv("SSH_HOST", "0.0.0.0")
	port := GetEnv("SSH_PORT", "2222")
	keyPath := GetEnv("SSH_AUTHORIZED_KEYS", "./ssh_data/authorized_keys")
	cfg := &Cfg{
		Logger: logger,
		PubSub: &BasicPubSub{logger: logger},
	}

	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%s", host, port)),
		wish.WithHostKeyPath("ssh_data/term_info_ed25519"),
		wish.WithAuthorizedKeys(keyPath),
		wish.WithMiddleware(PubSubMiddleware(cfg)),
	)
	if err != nil {
		logger.Error(err.Error())
		return
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	logger.Info(
		"starting SSH server",
		"host", host,
		"port", port,
	)
	go func() {
		if err = s.ListenAndServe(); err != nil {
			logger.Error(err.Error())
		}
	}()

	<-done
	logger.Info("stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer func() {
		cancel()
	}()
	if err := s.Shutdown(ctx); err != nil {
		logger.Error(err.Error())
	}
}
