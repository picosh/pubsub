package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/picosh/pubsub"
	"golang.org/x/crypto/ssh"
)

type MultiHandler struct {
	Handlers []slog.Handler
	mu       sync.Mutex
}

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

type PubSubLogWriter struct {
	SSHClient        *ssh.Client
	Session          *ssh.Session
	StdinPipe        io.WriteCloser
	Done             chan struct{}
	Messages         chan []byte
	Timeout          time.Duration
	BufferSize       int
	closeOnce        sync.Once
	closeMessageOnce sync.Once
	startOnce        sync.Once
	connecMu         sync.Mutex
	ConnectionInfo   *pubsub.RemoteClientInfo
}

func (c *PubSubLogWriter) Close() error {
	c.connecMu.Lock()
	defer c.connecMu.Unlock()

	if c.Done != nil {
		c.closeOnce.Do(func() {
			close(c.Done)
		})
	}

	if c.Messages != nil {
		c.closeMessageOnce.Do(func() {
			close(c.Messages)
		})
	}

	var errs []error

	if c.StdinPipe != nil {
		errs = append(errs, c.StdinPipe.Close())
	}

	if c.Session != nil {
		errs = append(errs, c.Session.Close())
	}

	if c.SSHClient != nil {
		errs = append(errs, c.SSHClient.Close())
	}

	return errors.Join(errs...)
}

func (c *PubSubLogWriter) Open() error {
	c.Close()

	c.connecMu.Lock()

	c.Done = make(chan struct{})
	c.Messages = make(chan []byte, c.BufferSize)

	sshClient, err := pubsub.CreateRemoteClient(c.ConnectionInfo)
	if err != nil {
		c.connecMu.Unlock()
		return err
	}

	session, err := sshClient.NewSession()
	if err != nil {
		c.connecMu.Unlock()
		return err
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		c.connecMu.Unlock()
		return err
	}

	err = session.Start("pub log-drain -b=false")
	if err != nil {
		c.connecMu.Unlock()
		return err
	}

	c.SSHClient = sshClient
	c.Session = session
	c.StdinPipe = stdinPipe

	c.closeOnce = sync.Once{}
	c.startOnce = sync.Once{}

	c.connecMu.Unlock()

	c.Start()

	return nil
}

func (c *PubSubLogWriter) Start() {
	c.startOnce.Do(func() {
		go func() {
			defer c.Reconnect()

			for {
				select {
				case data, ok := <-c.Messages:
					_, err := c.StdinPipe.Write(data)
					if !ok || err != nil {
						slog.Error("received error on write, reopening logger", "error", err)
						return
					}
				case <-c.Done:
					return
				}
			}
		}()
	})
}

func (c *PubSubLogWriter) Write(data []byte) (int, error) {
	var (
		n   int
		err error
	)

	ok := c.connecMu.TryLock()

	if !ok {
		return n, fmt.Errorf("unable to acquire lock to write")
	}

	defer c.connecMu.Unlock()

	if c.Messages == nil || c.Done == nil {
		return n, fmt.Errorf("logger not viable")
	}

	select {
	case c.Messages <- slices.Clone(data):
		n = len(data)
	case <-time.After(c.Timeout):
		err = fmt.Errorf("unable to send data within timeout")
	case <-c.Done:
		break
	}

	return n, err
}

func (c *PubSubLogWriter) Reconnect() {
	go func() {
		for {
			err := c.Open()
			if err != nil {
				slog.Error("unable to open send logger. retrying in 10 seconds", "error", err)
			} else {
				return
			}

			<-time.After(10 * time.Second)
		}
	}()
}

func SendLogRegister(logger *slog.Logger, connectionInfo *pubsub.RemoteClientInfo, buffer int) (*slog.Logger, error) {
	if buffer < 0 {
		buffer = 0
	}

	currentHandler := logger.Handler()

	logWriter := &PubSubLogWriter{
		Timeout:        10 * time.Millisecond,
		BufferSize:     buffer,
		ConnectionInfo: connectionInfo,
	}

	logWriter.Reconnect()

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

var _ io.Writer = (*PubSubLogWriter)(nil)
var _ slog.Handler = (*MultiHandler)(nil)

func ConnectToLogs(ctx context.Context, connectionInfo *pubsub.RemoteClientInfo) (io.Reader, error) {
	sshClient, err := pubsub.CreateRemoteClient(connectionInfo)
	if err != nil {
		return nil, err
	}

	session, err := sshClient.NewSession()
	if err != nil {
		return nil, err
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}

	err = session.Start("sub log-drain -k")
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		session.Close()
		sshClient.Close()
	}()

	return stdoutPipe, nil
}
