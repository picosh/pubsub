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

				err := cfg.PubSub.Sub(channel, sub)
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

				err := cfg.PubSub.Pub(channel, pub)
				if err != nil {
					logger.Error("error from pub", slog.Any("error", err), slog.String("pub", pub.ID))
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
			Logger:   logger,
			Channels: syncmap.New[string, *pubsub.Channel](),
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
				for _, channel := range cfg.PubSub.GetChannels("") {
					slog.Info("channel online", slog.Any("channel", channel.Name))
					for _, pub := range cfg.PubSub.GetPubs(channel.Name) {
						slog.Info("pub online", slog.Any("channel", channel.Name), slog.Any("pub", pub.ID))
					}
					for _, sub := range cfg.PubSub.GetSubs(channel.Name) {
						slog.Info("sub online", slog.Any("channel", channel.Name), slog.Any("sub", sub.ID))
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
