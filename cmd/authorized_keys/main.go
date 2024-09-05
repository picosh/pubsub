package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/picosh/pubsub"
)

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func PubSubMiddleware(cfg *pubsub.Cfg) wish.Middleware {
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
				sub := &pubsub.Subscriber{
					Name:    channel,
					Session: sesh,
					Chan:    make(chan error),
				}
				err := cfg.PubSub.Sub(sub)
				if err != nil {
					wish.Errorln(sesh, err)
				}
			} else if cmd == "pub" {
				msg := &pubsub.Msg{
					Name:   channel,
					Reader: sesh,
				}
				err := cfg.PubSub.Pub(msg)
				if err != nil {
					wish.Errorln(sesh, err)
				}
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
	cfg := &pubsub.Cfg{
		Logger: logger,
		PubSub: &pubsub.PubSubMulticast{
			Logger: logger,
			Chan:   make(chan *pubsub.Subscriber),
		},
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
