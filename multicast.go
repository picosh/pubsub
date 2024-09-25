package pubsub

import (
	"errors"
	"io"
	"log/slog"
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

var _ PubSub = &PubSubMulticast{}
var _ PubSub = (*PubSubMulticast)(nil)

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

func NewPipe(name string) *Pipe {
	return &Pipe{
		ID:      name,
		Clients: syncmap.New[string, *PipeClient](),
		Done:    make(chan struct{}),
		Data:    make(chan PipeMessage),
	}
}

func (b *PubSubMulticast) ensurePipe(pipe *Pipe) *Pipe {
	dataPipe, _ := b.Pipes.LoadOrStore(pipe.ID, pipe)
	dataPipe.Handle()
	return dataPipe
}

func (b *PubSubMulticast) GetPipes() []*Pipe {
	var pipes []*Pipe
	b.Pipes.Range(func(I string, J *Pipe) bool {
		pipes = append(pipes, J)
		return true
	})
	return pipes
}

func (b *PubSubMulticast) Pipe(pipeClient *PipeClient, pipes []*Pipe) error {
	var wg sync.WaitGroup
	var finErr error
	for _, p := range pipes {
		wg.Add(1)
		go func(pipe *Pipe) {
			readErr, writeErr := b._pipe(pipe, pipeClient)
			if writeErr != nil {
				slog.Error(
					"error writing to sub",
					slog.String("pipeClient", pipeClient.ID),
					slog.String("pipe", pipe.ID),
					slog.Any("error", writeErr),
				)
				finErr = errors.Join(finErr, writeErr)
			}

			if readErr != nil {
				slog.Error(
					"error reading from pipe",
					slog.String("pipeClient", pipeClient.ID),
					slog.String("pipe", pipe.ID),
					slog.Any("error", readErr),
				)
				finErr = errors.Join(finErr, writeErr)
			}
			wg.Done()
		}(p)
	}
	wg.Wait()
	return finErr
}

func (b *PubSubMulticast) _pipe(pipe *Pipe, pipeClient *PipeClient) (error, error) {
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

				readErr = err
				return
			}
		}
	}()

	wg.Wait()

	return readErr, writeErr
}

func (b *PubSubMulticast) GetChannels() []*Channel {
	var chans []*Channel
	b.Channels.Range(func(I string, J *Channel) bool {
		chans = append(chans, J)
		return true
	})
	return chans
}

func (b *PubSubMulticast) GetChannel(channel string) *Channel {
	channelData, _ := b.Channels.Load(channel)
	return channelData
}

func (b *PubSubMulticast) GetPubs() []*Pub {
	var pubs []*Pub
	for _, channel := range b.GetChannels() {
		channel.Pubs.Range(func(K string, V *Pub) bool {
			pubs = append(pubs, V)
			return true
		})
	}
	return pubs
}

func (b *PubSubMulticast) GetSubs() []*Sub {
	var subs []*Sub
	for _, channel := range b.GetChannels() {
		channel.Subs.Range(func(K string, V *Sub) bool {
			subs = append(subs, V)
			return true
		})
	}
	return subs
}

func NewChannel(name string) *Channel {
	return &Channel{
		ID:   name,
		Done: make(chan struct{}),
		Data: make(chan []byte),
		Subs: syncmap.New[string, *Sub](),
		Pubs: syncmap.New[string, *Pub](),
	}
}

func (b *PubSubMulticast) ensureChannel(channel *Channel) *Channel {
	dataChannel, _ := b.Channels.LoadOrStore(channel.ID, channel)
	dataChannel.Handle()
	return dataChannel
}

func (b *PubSubMulticast) _sub(channel *Channel, sub *Sub) error {
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
				return err
			}

			if !ok {
				break mainLoop
			}
		}
	}

	return nil
}

func (b *PubSubMulticast) Sub(sub *Sub, channels []*Channel) error {
	var wg sync.WaitGroup
	var finErr error
	for _, ch := range channels {
		wg.Add(1)
		go func(channel *Channel) {
			err := b._sub(channel, sub)
			if err != nil {
				b.Logger.Error(
					"error writing to sub",
					slog.String("sub", sub.ID),
					slog.String("channel", channel.ID),
					slog.Any("error", err),
				)
				finErr = errors.Join(finErr, err)
			}
			wg.Done()
		}(ch)
	}
	wg.Wait()
	return finErr
}

func (b *PubSubMulticast) Pub(pub *Pub, channels []*Channel) error {
	var wg sync.WaitGroup
	var finErr error
	for _, ch := range channels {
		wg.Add(1)
		go func(channel *Channel) {
			err := b._pub(channel, pub)
			if err != nil {
				b.Logger.Error(
					"error writing to sub",
					slog.String("pub", pub.ID),
					slog.String("channel", channel.ID),
					slog.Any("error", err),
				)
				finErr = errors.Join(finErr, err)
			}
			wg.Done()
		}(ch)
	}
	wg.Wait()
	return finErr
}

func (b *PubSubMulticast) _pub(channel *Channel, pub *Pub) error {
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

				slog.Error("error reading from pub", slog.String("pub", pub.ID), slog.String("channel", channel.ID), slog.Any("error", err))
				return err
			}
		}
	}

	return nil
}
