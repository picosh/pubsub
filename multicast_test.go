package pubsub

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/antoniomika/syncmap"
)

func TestMulticastSubBlock(t *testing.T) {
	orderActual := ""
	orderExpected := "sub-pub-"
	actual := new(bytes.Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger:   slog.Default(),
		Channels: syncmap.New[string, *Channel](),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		sub := &Sub{
			ID:     "1",
			Writer: actual,
			Done:   make(chan struct{}),
			Data:   make(chan []byte),
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(name, sub))
		wg.Done()
	}()

	<-syncer

	go func() {
		pub := &Pub{
			ID:     "1",
			Done:   make(chan struct{}),
			Reader: strings.NewReader(expected),
		}
		orderActual += "pub-"
		fmt.Println(cast.Pub(name, pub))
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
	actual := new(bytes.Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger:   slog.Default(),
		Channels: syncmap.New[string, *Channel](),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		pub := &Pub{
			ID:     "1",
			Done:   make(chan struct{}),
			Reader: strings.NewReader(expected),
		}
		orderActual += "pub-"
		syncer <- 0
		fmt.Println(cast.Pub(name, pub))
		wg.Done()
	}()

	<-syncer

	go func() {
		sub := &Sub{
			ID:     "1",
			Writer: actual,
			Done:   make(chan struct{}),
			Data:   make(chan []byte),
		}
		orderActual += "sub-"
		wg.Done()
		fmt.Println(cast.Sub(name, sub))
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
	actual := new(bytes.Buffer)
	actualOther := new(bytes.Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger:   slog.Default(),
		Channels: syncmap.New[string, *Channel](),
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		sub := &Sub{
			ID:     "1",
			Writer: actual,
			Done:   make(chan struct{}),
			Data:   make(chan []byte),
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(name, sub))
		wg.Done()
	}()

	<-syncer

	go func() {
		sub := &Sub{
			ID:     "2",
			Writer: actualOther,
			Done:   make(chan struct{}),
			Data:   make(chan []byte),
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(name, sub))
		wg.Done()
	}()

	<-syncer

	go func() {
		pub := &Pub{
			ID:     "1",
			Done:   make(chan struct{}),
			Reader: strings.NewReader(expected),
		}
		orderActual += "pub-"
		fmt.Println(cast.Pub(name, pub))
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
