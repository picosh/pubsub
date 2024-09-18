package pubsub

import (
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/antoniomika/syncmap"
)

type Channel struct {
	Name        string
	Done        chan struct{}
	Data        chan []byte
	Chan        chan *Sub
	Subs        *syncmap.Map[string, *Sub]
	Pubs        *syncmap.Map[string, *Pub]
	once        sync.Once
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
	c.once.Do(func() {
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
							slog.Error("timeout writing to sub", slog.Any("sub", I), slog.Any("channel", c.Name))
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
	Error    chan error
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

func (sub *Sub) Wait() error {
	select {
	case err := <-sub.Error:
		return err
	case <-sub.Done:
		return nil
	}
}

type Pub struct {
	ID     string
	Done   chan struct{}
	Reader io.Reader
	once   sync.Once
	SentAt *time.Time
}

func (pub *Pub) Cleanup() {
	pub.once.Do(func() {
		close(pub.Done)
	})
}

type PubSub interface {
	GetSubs(channel string) []*Sub
	GetPubs(channel string) []*Pub
	GetChannels(channelPrefix string) []*Channel
	GetChannel(channel string) *Channel
	Sub(channel string, sub *Sub) error
	Pub(channel string, pub *Pub) error
}

type Cfg struct {
	Logger *slog.Logger
	PubSub PubSub
}
