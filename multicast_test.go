package pubsub

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/antoniomika/syncmap"
)

type Buffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (b *Buffer) Read(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Read(p)
}
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.Write(p)
}
func (b *Buffer) String() string {
	b.m.Lock()
	defer b.m.Unlock()
	return b.b.String()
}

func TestMulticastSubBlock(t *testing.T) {
	orderActual := ""
	orderExpected := "sub-pub-"
	actual := new(Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger: slog.Default(),
		BaseConnector: &BaseConnector{
			Channels: syncmap.New[string, *Channel](),
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	channel := NewChannel(name)

	go func() {
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(context.TODO(), "1", actual, []*Channel{channel}))
		wg.Done()
	}()

	<-syncer

	go func() {
		orderActual += "pub-"
		fmt.Println(cast.Pub(context.TODO(), "2", &Buffer{b: *bytes.NewBufferString(expected)}, []*Channel{channel}))
		wg.Done()
	}()

	wg.Wait()

	if orderActual != orderExpected {
		t.Fatalf("\norderActual:(%s)\norderExpected:(%s)", orderActual, orderExpected)
	}
	if actual.String() != expected {
		t.Fatalf("\nactual:(%s)\nexpected:(%s)", actual, expected)
	}
}

func TestMulticastPubBlock(t *testing.T) {
	orderActual := ""
	orderExpected := "pub-sub-"
	actual := new(Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger: slog.Default(),
		BaseConnector: &BaseConnector{
			Channels: syncmap.New[string, *Channel](),
		},
	}

	var wg sync.WaitGroup
	wg.Add(2)

	channel := NewChannel(name)

	go func() {
		orderActual += "pub-"
		syncer <- 0
		fmt.Println(cast.Pub(context.TODO(), "1", &Buffer{b: *bytes.NewBufferString(expected)}, []*Channel{channel}))
		wg.Done()
	}()

	<-syncer

	go func() {
		orderActual += "sub-"
		wg.Done()
		fmt.Println(cast.Sub(context.TODO(), "2", actual, []*Channel{channel}))
	}()

	wg.Wait()

	if orderActual != orderExpected {
		t.Fatalf("\norderActual:(%s)\norderExpected:(%s)", orderActual, orderExpected)
	}
	if actual.String() != expected {
		t.Fatalf("\nactual:(%s)\nexpected:(%s)", actual, expected)
	}
}

func TestMulticastMultSubs(t *testing.T) {
	orderActual := ""
	orderExpected := "sub-sub-pub-"
	actual := new(Buffer)
	actualOther := new(Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger: slog.Default(),
		BaseConnector: &BaseConnector{
			Channels: syncmap.New[string, *Channel](),
		},
	}

	var wg sync.WaitGroup
	wg.Add(3)

	channel := NewChannel(name)

	go func() {
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(context.TODO(), "1", actual, []*Channel{channel}))
		wg.Done()
	}()

	<-syncer

	go func() {
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(context.TODO(), "2", actualOther, []*Channel{channel}))
		wg.Done()
	}()

	<-syncer

	go func() {
		orderActual += "pub-"
		fmt.Println(cast.Pub(context.TODO(), "3", &Buffer{b: *bytes.NewBufferString(expected)}, []*Channel{channel}))
		wg.Done()
	}()

	wg.Wait()

	if orderActual != orderExpected {
		t.Fatalf("\norderActual:(%s)\norderExpected:(%s)", orderActual, orderExpected)
	}
	if actual.String() != expected {
		t.Fatalf("\nactual:(%s)\nexpected:(%s)", actual, expected)
	}
	if actualOther.String() != expected {
		t.Fatalf("\nactual:(%s)\nexpected:(%s)", actualOther, expected)
	}
}
