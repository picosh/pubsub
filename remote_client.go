package pubsub

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

type RemoteClientInfo struct {
	RemoteHost     string
	KeyLocation    string
	KeyPassphrase  string
	RemoteHostname string
	RemoteUser     string
}

func CreateRemoteClient(info *RemoteClientInfo) (*ssh.Client, error) {
	if info == nil {
		return nil, fmt.Errorf("connection info is invalid")
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
