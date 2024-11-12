package log

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"

	"github.com/picosh/pubsub"
)

type MultiHandler struct {
	Handlers []slog.Handler
	mu       sync.Mutex
}

var _ slog.Handler = (*MultiHandler)(nil)

func (m *MultiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, h := range m.Handlers {
		if h.Enabled(ctx, l) {
			return true
		}
	}

	return false
}

func (m *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, h := range m.Handlers {
		if h.Enabled(ctx, r.Level) {
			errs = append(errs, h.Handle(ctx, r.Clone()))
		}
	}

	return errors.Join(errs...)
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	m.mu.Lock()
	defer m.mu.Unlock()

	var handlers []slog.Handler

	for _, h := range m.Handlers {
		handlers = append(handlers, h.WithAttrs(slices.Clone(attrs)))
	}

	return &MultiHandler{
		Handlers: handlers,
	}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return m
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var handlers []slog.Handler

	for _, h := range m.Handlers {
		handlers = append(handlers, h.WithGroup(name))
	}

	return &MultiHandler{
		Handlers: handlers,
	}
}

func SendLogRegister(logger *slog.Logger, info *pubsub.RemoteClientInfo, buffer int) (*slog.Logger, error) {
	if buffer < 0 {
		buffer = 0
	}

	logWriter := pubsub.NewRemoteClientWriter(info, logger, buffer)
	go logWriter.KeepAlive("pub log-drain -b=false")

	currentHandler := logger.Handler()
	return slog.New(
		&MultiHandler{
			Handlers: []slog.Handler{
				currentHandler,
				slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
					AddSource: true,
					Level:     slog.LevelDebug,
				}),
			},
		},
	), nil
}

func ConnectToLogs(ctx context.Context, connectionInfo *pubsub.RemoteClientInfo) (io.Reader, error) {
	return pubsub.RemoteSub("sub log-drain -k", ctx, connectionInfo)
}
