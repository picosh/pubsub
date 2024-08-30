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
	logger *slog.Logger
	subs   []*Subscriber
}

func (b *PubSubMulticast) GetSubs() []*Subscriber {
	b.logger.Info("getsubs")
	return b.subs
}

func (b *PubSubMulticast) Sub(sub *Subscriber) error {
	id := uuid.New()
	sub.ID = id.String()
	b.logger.Info("sub", "channel", sub.Name, "id", id)
	b.subs = append(b.subs, sub)
	return sub.Wait()
}

func (b *PubSubMulticast) UnSub(rm *Subscriber) error {
	b.logger.Info("unsub", "channel", rm.Name, "id", rm.ID)
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
	log := b.logger.With("channel", msg.Name)
	log.Info("pub")

	matches := []*Subscriber{}
	writers := []io.Writer{}
	for _, sub := range b.subs {
		if sub.Name == msg.Name {
			matches = append(matches, sub)
			writers = append(writers, sub.Session)
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

func main() {
	logger := slog.Default()
	host := GetEnv("SSH_HOST", "0.0.0.0")
	port := GetEnv("SSH_PORT", "2222")
	keyPath := GetEnv("SSH_AUTHORIZED_KEYS", "./ssh_data/authorized_keys")
	cfg := &Cfg{
		Logger: logger,
		PubSub: &PubSubMulticast{logger: logger},
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
