package pubsub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type RemoteClientWriter struct {
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
	Info             *RemoteClientInfo
	Logger           *slog.Logger
}

var _ io.Writer = (*RemoteClientWriter)(nil)

func NewRemoteClientWriter(info *RemoteClientInfo, logger *slog.Logger, buffer int) *RemoteClientWriter {
	return &RemoteClientWriter{
		Timeout:    10 * time.Millisecond,
		Info:       info,
		BufferSize: buffer,
		Logger:     logger,
	}
}

func (c *RemoteClientWriter) Close() error {
	c.Logger.Info("closing ssh conn", "host", c.Info.RemoteHost)
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

func (c *RemoteClientWriter) Open(cmd string) error {
	c.Close()

	c.connecMu.Lock()

	c.Done = make(chan struct{})
	c.Messages = make(chan []byte, c.BufferSize)

	c.Logger.Info("connecting to ssh conn", "host", c.Info.RemoteHost, "cmd", cmd)
	sshClient, err := CreateRemoteClient(c.Info)
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

	err = session.Start(cmd)
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

	c.start()

	return nil
}

func (c *RemoteClientWriter) start() {
	c.startOnce.Do(func() {
		go func() {
			for {
				select {
				case data, ok := <-c.Messages:
					_, err := c.StdinPipe.Write(data)
					if !ok || err != nil {
						c.Logger.Error("received error on write, reopening conn", "error", err)
						return
					}
				case <-c.Done:
					return
				}
			}
		}()
	})
}

func (c *RemoteClientWriter) Write(data []byte) (int, error) {
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
		return n, fmt.Errorf("conn not viable")
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

func (c *RemoteClientWriter) KeepAlive(cmd string) {
	for {
		err := c.Open(cmd)
		if err != nil {
			c.Logger.Error("unable to open send to ssh conn. retrying in 10 seconds", "error", err)
		} else {
			return
		}

		<-time.After(10 * time.Second)
	}
}

type RemoteClientInfo struct {
	RemoteHost     string
	KeyLocation    string
	KeyPassphrase  string
	RemoteHostname string
	RemoteUser     string
}

func CreateRemoteClient(info *RemoteClientInfo) (*ssh.Client, error) {
	if info == nil {
		return nil, fmt.Errorf("conn info is invalid")
	}

	if !strings.Contains(info.RemoteHost, ":") {
		info.RemoteHost += ":22"
	}

	rawConn, err := net.Dial("tcp", info.RemoteHost)
	if err != nil {
		return nil, err
	}

	keyPath, err := filepath.Abs(info.KeyLocation)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(keyPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var signer ssh.Signer

	if info.KeyPassphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(info.KeyPassphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(data)
	}

	if err != nil {
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(rawConn, info.RemoteHostname, &ssh.ClientConfig{
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		User:            info.RemoteUser,
	})

	if err != nil {
		return nil, err
	}

	sshClient := ssh.NewClient(sshConn, chans, reqs)

	return sshClient, nil
}

func RemoteSub(cmd string, ctx context.Context, info *RemoteClientInfo) (io.Reader, error) {
	sshClient, err := CreateRemoteClient(info)
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

	err = session.Start(cmd)
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

func RemotePub(cmd string, ctx context.Context, info *RemoteClientInfo) (io.WriteCloser, error) {
	sshClient, err := CreateRemoteClient(info)
	if err != nil {
		return nil, err
	}

	session, err := sshClient.NewSession()
	if err != nil {
		return nil, err
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}

	err = session.Start(cmd)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		session.Close()
		sshClient.Close()
	}()

	return stdinPipe, err
}
