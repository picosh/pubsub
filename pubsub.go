package pubsub

import (
	"io"
	"iter"
	"log/slog"
	"sync"
	"time"

	"github.com/antoniomika/syncmap"
)

type Channel struct {
	ID          string
	Done        chan struct{}
	Data        chan []byte
	Subs        *syncmap.Map[string, *Sub]
	Pubs        *syncmap.Map[string, *Pub]
	handleOnce  sync.Once
	cleanupOnce sync.Once
	onceData    sync.Once
}

func (c *Channel) Cleanup() {
	c.cleanupOnce.Do(func() {
		close(c.Done)
		c.onceData.Do(func() {
			close(c.Data)
		})
	})
}

func (c *Channel) Handle() {
	c.handleOnce.Do(func() {
		go func() {
			defer func() {
				c.Subs.Range(func(I string, J *Sub) bool {
					J.Cleanup()
					return true
				})

				c.Pubs.Range(func(I string, J *Pub) bool {
					J.Cleanup()
					return true
				})
			}()

		mainLoop:
			for {
				select {
				case <-c.Done:
					return
				case data, ok := <-c.Data:
					count := 0
					for count == 0 {
						c.Subs.Range(func(I string, J *Sub) bool {
							count++
							return true
						})
						if count == 0 {
							select {
							case <-time.After(1 * time.Millisecond):
							case <-c.Done:
								break mainLoop
							}
						}
					}

					c.Subs.Range(func(I string, J *Sub) bool {
						if !ok {
							J.onceData.Do(func() {
								close(J.Data)
							})
							return true
						}

						select {
						case J.Data <- data:
							return true
						case <-J.Done:
							return true
						case <-c.Done:
							return true
						case <-time.After(1 * time.Second):
							slog.Error("timeout writing to sub", slog.Any("sub", I), slog.Any("channel", c.ID))
							return true
						}
					})
				}
			}
		}()
	})
}

type Sub struct {
	ID       string
	Done     chan struct{}
	Data     chan []byte
	Writer   io.Writer
	once     sync.Once
	onceData sync.Once
}

func (sub *Sub) Cleanup() {
	sub.once.Do(func() {
		close(sub.Done)
		sub.onceData.Do(func() {
			close(sub.Data)
		})
	})
}

type Pub struct {
	ID     string
	Done   chan struct{}
	Reader io.Reader
	once   sync.Once
}

func (pub *Pub) Cleanup() {
	pub.once.Do(func() {
		close(pub.Done)
	})
}

type PipeClient struct {
	ID         string
	Done       chan struct{}
	Data       chan PipeMessage
	ReadWriter io.ReadWriter
	Replay     bool
	once       sync.Once
	onceData   sync.Once
}

func (pipeClient *PipeClient) Cleanup() {
	pipeClient.once.Do(func() {
		close(pipeClient.Done)
	})
}

type PipeMessage struct {
	Data      []byte
	ClientID  string
	Direction PipeDirection
}

type Pipe struct {
	ID          string
	Clients     *syncmap.Map[string, *PipeClient]
	Done        chan struct{}
	Data        chan PipeMessage
	handleOnce  sync.Once
	cleanupOnce sync.Once
}

func (pipe *Pipe) Handle() {
	pipe.handleOnce.Do(func() {
		go func() {
			defer func() {
				pipe.Clients.Range(func(I string, J *PipeClient) bool {
					J.Cleanup()
					return true
				})
			}()

			for {
				select {
				case <-pipe.Done:
					return
				case data, ok := <-pipe.Data:
					pipe.Clients.Range(func(I string, J *PipeClient) bool {
						if !ok {
							J.onceData.Do(func() {
								close(J.Data)
							})
							return true
						}

						data.Direction = PipeOutput

						select {
						case J.Data <- data:
							return true
						case <-J.Done:
							return true
						case <-pipe.Done:
							return true
						case <-time.After(1 * time.Second):
							slog.Error("timeout writing to pipe", slog.String("pipeClient", I), slog.String("pipe", pipe.ID))
							return true
						}
					})
				case <-time.After(1 * time.Millisecond):
					count := 0
					pipe.Clients.Range(func(I string, J *PipeClient) bool {
						count++
						return true
					})
					if count == 0 {
						return
					}
				}
			}
		}()
	})
}

func (pipe *Pipe) Cleanup() {
	pipe.cleanupOnce.Do(func() {
		close(pipe.Done)
		close(pipe.Data)
	})
}

type PubSub interface {
	GetSubs() iter.Seq[*Sub]
	GetPubs() iter.Seq[*Pub]
	GetChannels() iter.Seq[*Channel]
	GetPipes() iter.Seq[*Pipe]
	Pipe(pipeClient *PipeClient, pipes []*Pipe) error
	Sub(sub *Sub, channels []*Channel) error
	Pub(pub *Pub, channels []*Channel) error
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}
