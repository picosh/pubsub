package pubsub

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/antoniomika/syncmap"
)

type PipeDirection int

const (
	PipeInput PipeDirection = iota
	PipeOutput
)

type PubSubMulticast struct {
	Logger   *slog.Logger
	Channels *syncmap.Map[string, *Channel]
	Pipes    *syncmap.Map[string, *Pipe]
}

func (b *PubSubMulticast) Cleanup() {
	toRemove := []string{}
	b.Channels.Range(func(I string, J *Channel) bool {
		count := 0
		J.Pubs.Range(func(K string, V *Pub) bool {
			count++
			return true
		})

		J.Subs.Range(func(K string, V *Sub) bool {
			count++
			return true
		})

		if count == 0 {
			J.Cleanup()
			toRemove = append(toRemove, I)
		}

		return true
	})

	for _, channel := range toRemove {
		b.Channels.Delete(channel)
	}

	pipesToRemove := []string{}
	b.Pipes.Range(func(I string, J *Pipe) bool {
		count := 0
		J.Clients.Range(func(K string, V *PipeClient) bool {
			count++
			return true
		})

		if count == 0 {
			J.Cleanup()
			pipesToRemove = append(pipesToRemove, I)
		}

		return true
	})

	for _, pipe := range pipesToRemove {
		b.Pipes.Delete(pipe)
	}
}

func (b *PubSubMulticast) ensurePipe(pipe string) *Pipe {
	dataPipe, _ := b.Pipes.LoadOrStore(pipe, &Pipe{
		Name:    pipe,
		Clients: syncmap.New[string, *PipeClient](),
		Done:    make(chan struct{}),
		Data:    make(chan PipeMessage),
	})
	dataPipe.Handle()

	return dataPipe
}

func (b *PubSubMulticast) GetPipes(pipePrefix string) []*Pipe {
	var pipes []*Pipe
	b.Pipes.Range(func(I string, J *Pipe) bool {
		if strings.HasPrefix(I, pipePrefix) {
			pipes = append(pipes, J)
		}

		return true
	})
	return pipes
}

func (b *PubSubMulticast) GetPipe(pipe string) *Pipe {
	pipeData, _ := b.Pipes.Load(pipe)
	return pipeData
}

func (b *PubSubMulticast) Pipe(pipe string, pipeClient *PipeClient) (error, error) {
	pipeData := b.ensurePipe(pipe)
	pipeData.Clients.Store(pipeClient.ID, pipeClient)
	defer func() {
		pipeClient.Cleanup()
		pipeData.Clients.Delete(pipeClient.ID)
		b.Cleanup()
	}()

	var (
		readErr  error
		writeErr error
		wg       sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
	mainLoop:
		for {
			select {
			case data, ok := <-pipeClient.Data:
				if data.Direction == PipeInput {
					select {
					case pipeData.Data <- data:
					case <-pipeClient.Done:
						break mainLoop
					case <-pipeData.Done:
						break mainLoop
					default:
						continue
					}
				} else {
					if data.ClientID == pipeClient.ID && !pipeClient.Replay {
						continue
					}

					_, err := pipeClient.ReadWriter.Write(data.Data)
					if err != nil {
						slog.Error("error writing to sub", slog.String("pipeClient", pipeClient.ID), slog.String("pipe", pipe), slog.Any("error", err))
						writeErr = err
						return
					}
				}

				if !ok {
					break mainLoop
				}
			case <-pipeClient.Done:
				break mainLoop
			case <-pipeData.Done:
				break mainLoop
			}
		}
	}()

	go func() {
		defer wg.Done()
	mainLoop:
		for {
			data := make([]byte, 32*1024)
			n, err := pipeClient.ReadWriter.Read(data)
			data = data[:n]

			pipeMessage := PipeMessage{
				Data:      data,
				ClientID:  pipeClient.ID,
				Direction: PipeInput,
			}

			select {
			case pipeClient.Data <- pipeMessage:
			case <-pipeClient.Done:
				break mainLoop
			case <-pipeData.Done:
				break mainLoop
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}

				slog.Error("error reading from pipe", slog.String("pipeClient", pipeClient.ID), slog.String("pipe", pipe), slog.Any("error", err))
				readErr = err
				return
			}
		}
	}()

	wg.Wait()

	return readErr, writeErr
}

func (b *PubSubMulticast) GetChannels(channelPrefix string) []*Channel {
	var chans []*Channel
	b.Channels.Range(func(I string, J *Channel) bool {
		if strings.HasPrefix(I, channelPrefix) {
			chans = append(chans, J)
		}

		return true
	})
	return chans
}

func (b *PubSubMulticast) GetChannel(channel string) *Channel {
	channelData, _ := b.Channels.Load(channel)
	return channelData
}

func (b *PubSubMulticast) GetPubs(channel string) []*Pub {
	var pubs []*Pub
	b.Channels.Range(func(I string, J *Channel) bool {
		found := channel == I
		if found || channel == "*" {
			J.Pubs.Range(func(K string, V *Pub) bool {
				pubs = append(pubs, V)
				return true
			})
		}

		return !found
	})
	return pubs
}

func (b *PubSubMulticast) GetSubs(channel string) []*Sub {
	var subs []*Sub
	b.Channels.Range(func(I string, J *Channel) bool {
		found := channel == I
		if found || channel == "*" {
			J.Subs.Range(func(K string, V *Sub) bool {
				subs = append(subs, V)
				return true
			})
		}

		return !found
	})
	return subs
}

func (b *PubSubMulticast) ensureChannel(channel string) *Channel {
	dataChannel, _ := b.Channels.LoadOrStore(channel, &Channel{
		Name: channel,
		Done: make(chan struct{}),
		Data: make(chan []byte),
		Subs: syncmap.New[string, *Sub](),
		Pubs: syncmap.New[string, *Pub](),
	})
	dataChannel.Handle()

	return dataChannel
}

func (b *PubSubMulticast) Sub(channel string, sub *Sub) error {
	dataChannel := b.ensureChannel(channel)
	dataChannel.Subs.Store(sub.ID, sub)
	defer func() {
		sub.Cleanup()
		dataChannel.Subs.Delete(sub.ID)
		b.Cleanup()
	}()

mainLoop:
	for {
		select {
		case <-sub.Done:
			break mainLoop
		case <-dataChannel.Done:
			break mainLoop
		case data, ok := <-sub.Data:
			_, err := sub.Writer.Write(data)
			if err != nil {
				slog.Error("error writing to sub", slog.String("sub", sub.ID), slog.String("channel", channel), slog.Any("error", err))
				return err
			}

			if !ok {
				break mainLoop
			}
		}
	}

	return nil
}

func (b *PubSubMulticast) Pub(channel string, pub *Pub) error {
	dataChannel := b.ensureChannel(channel)
	dataChannel.Pubs.Store(pub.ID, pub)
	defer func() {
		pub.Cleanup()
		dataChannel.Pubs.Delete(pub.ID)

		count := 0
		dataChannel.Pubs.Range(func(I string, J *Pub) bool {
			count++
			return true
		})

		if count == 0 {
			dataChannel.onceData.Do(func() {
				close(dataChannel.Data)
			})
		}

		b.Cleanup()
	}()

mainLoop:
	for {
		select {
		case <-pub.Done:
			break mainLoop
		case <-dataChannel.Done:
			break mainLoop
		default:
			data := make([]byte, 32*1024)
			n, err := pub.Reader.Read(data)
			data = data[:n]

			select {
			case dataChannel.Data <- data:
			case <-pub.Done:
				break mainLoop
			case <-dataChannel.Done:
				break mainLoop
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}

				slog.Error("error reading from pub", slog.String("pub", pub.ID), slog.String("channel", channel), slog.Any("error", err))
				return err
			}
		}
	}

	return nil
}
