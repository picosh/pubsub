package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/antoniomika/syncmap"
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

func PubSubMiddleware(cfg *pubsub.Cfg) wish.Middleware {
	return func(next ssh.Handler) ssh.Handler {
		return func(sesh ssh.Session) {
			args := sesh.Command()
			if len(args) < 2 {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {channel}")
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
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {channel}")
			} else if cmd == "sub" {
				sub := &pubsub.Sub{
					ID:     uuid.NewString(),
					Writer: sesh,
					Done:   make(chan struct{}),
					Data:   make(chan []byte),
				}

				go func() {
					<-sesh.Context().Done()
					sub.Cleanup()
				}()

				channel := pubsub.NewChannel(channel)
				err := cfg.PubSub.Sub(sub, []*pubsub.Channel{channel})
				if err != nil {
					logger.Error("error from sub", slog.Any("error", err), slog.String("sub", sub.ID))
				}
			} else if cmd == "pub" {
				pub := &pubsub.Pub{
					ID:     uuid.NewString(),
					Done:   make(chan struct{}),
					Reader: sesh,
				}

				go func() {
					<-sesh.Context().Done()
					pub.Cleanup()
				}()

				channel := pubsub.NewChannel(channel)
				err := cfg.PubSub.Pub(pub, []*pubsub.Channel{channel})
				if err != nil {
					logger.Error("error from pub", slog.Any("error", err), slog.String("pub", pub.ID))
				}
			} else if cmd == "pipe" {
				pipeClient := &pubsub.PipeClient{
					ID:         uuid.NewString(),
					Done:       make(chan struct{}),
					Data:       make(chan pubsub.PipeMessage),
					Replay:     args[len(args)-1] == "replay",
					ReadWriter: sesh,
				}

				go func() {
					<-sesh.Context().Done()
					pipeClient.Cleanup()
				}()

				pipe := pubsub.NewPipe(channel)
				err := cfg.PubSub.Pipe(pipeClient, []*pubsub.Pipe{pipe})
				if err != nil {
					logger.Error(
						"pipe error",
						slog.Any("error", err),
						slog.String("pipeClient", pipeClient.ID),
					)
				}
			} else {
				wish.Println(sesh, "USAGE: ssh send.pico.sh (sub|pub|pipe) {channel}")
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
			Logger:   logger,
			Channels: syncmap.New[string, *pubsub.Channel](),
			Pipes:    syncmap.New[string, *pubsub.Pipe](),
		},
	}

	s, err := wish.NewServer(
		ssh.NoPty(),
		wish.WithAddress(fmt.Sprintf("%s:%s", host, port)),
		wish.WithHostKeyPath("ssh_data/term_info_ed25519"),
		wish.WithAuthorizedKeys(keyPath),
		wish.WithMiddleware(
			PubSubMiddleware(cfg),
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
				for _, channel := range cfg.PubSub.GetChannels() {
					slog.Info("channel online", slog.Any("channel", channel.ID))
					for _, pub := range cfg.PubSub.GetPubs() {
						if pub.ID == channel.ID {
							slog.Info("pub online", slog.Any("channel", channel.ID), slog.Any("pub", pub.ID))
						}
					}
					for _, sub := range cfg.PubSub.GetSubs() {
						if sub.ID == channel.ID {
							slog.Info("sub online", slog.Any("channel", channel.ID), slog.Any("sub", sub.ID))
						}
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
