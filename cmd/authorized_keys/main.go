package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/google/uuid"
	"github.com/picosh/pubsub"
)

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func PubSubMiddleware(broker pubsub.PubSub, logger *slog.Logger) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
			if len(args) < 2 {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {topic}")
				next(sesh)
				return
			}

			cmd := strings.TrimSpace(args[0])
			topicsRaw := args[1]

			topics := strings.Split(topicsRaw, ",")

			logger := logger.With(
				"cmd", cmd,
				"topics", topics,
			)

			logger.Info("running cli")

			if cmd == "help" {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {topic}")
			} else if cmd == "sub" {
				var chans []*pubsub.Channel

				for _, topic := range topics {
					chans = append(chans, pubsub.NewChannel(topic))
				}

				clientID := uuid.NewString()

				err := errors.Join(broker.Sub(sesh.Context(), clientID, sesh, chans, args[len(args)-1] == "keepalive"))
				if err != nil {
					logger.Error("error during pub", slog.Any("error", err), slog.String("client", clientID))
				}
			} else if cmd == "pub" {
				var chans []*pubsub.Channel

				for _, topic := range topics {
					chans = append(chans, pubsub.NewChannel(topic))
				}

				clientID := uuid.NewString()

				err := errors.Join(broker.Pub(sesh.Context(), clientID, sesh, chans))
				if err != nil {
					logger.Error("error during pub", slog.Any("error", err), slog.String("client", clientID))
				}
			} else if cmd == "pipe" {
				var chans []*pubsub.Channel

				for _, topics := range topics {
					chans = append(chans, pubsub.NewChannel(topics))
				}

				clientID := uuid.NewString()

				err := errors.Join(broker.Pipe(sesh.Context(), clientID, sesh, chans, args[len(args)-1] == "replay"))
				if err != nil {
					logger.Error(
						"pipe error",
						slog.Any("error", err),
						slog.String("pipeClient", clientID),
					)
				}
			} else {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {topic}")
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
	broker := pubsub.NewMulticast(logger)

	s, err := wish.NewServer(
		ssh.NoPty(),
		wish.WithAddress(fmt.Sprintf("%s:%s", host, port)),
		wish.WithHostKeyPath("ssh_data/term_info_ed25519"),
		wish.WithAuthorizedKeys(keyPath),
		wish.WithMiddleware(
			PubSubMiddleware(broker, logger),
		),
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

	go func() {
		for {
			slog.Info("Debug Info", slog.Int("goroutines", runtime.NumGoroutine()))
			select {
			case <-time.After(5 * time.Second):
				for _, channel := range broker.GetChannels() {
					slog.Info("channel online", slog.Any("channel topic", channel.Topic))
					for _, client := range channel.GetClients() {
						slog.Info("client online", slog.Any("channel topic", channel.Topic), slog.Any("client", client.ID), slog.String("direction", client.Direction.String()))
					}
				}
			case <-done:
				return
			}
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
