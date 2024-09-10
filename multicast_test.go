package pubsub

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestMulticastSubBlock(t *testing.T) {
	orderActual := ""
	orderExpected := "sub-pub-"
	actual := new(bytes.Buffer)
	expected := "some test data"
	name := "test-channel"
	syncer := make(chan int)

	cast := &PubSubMulticast{
		Logger: slog.Default(),
		Chan:   make(chan *Subscriber),
	}

	go func() {
		sub := &Subscriber{
			ID:     "1",
			Name:   name,
			Chan:   make(chan error),
			Writer: actual,
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(sub))
	}()

	<-syncer

	go func() {
		msg := &Msg{Name: name, Reader: strings.NewReader(expected)}
		orderActual += "pub-"
		syncer <- 0
		fmt.Println(cast.Pub(msg))
	}()

	<-syncer

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
		Logger: slog.Default(),
		Chan:   make(chan *Subscriber),
	}

	go func() {
		msg := &Msg{Name: name, Reader: strings.NewReader(expected)}
		orderActual += "pub-"
		syncer <- 0
		fmt.Println(cast.Pub(msg))
	}()

	<-syncer

	go func() {
		sub := &Subscriber{
			ID:     "1",
			Name:   name,
			Chan:   make(chan error),
			Writer: actual,
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(sub))
	}()

	<-syncer

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
		Logger: slog.Default(),
		Chan:   make(chan *Subscriber),
	}

	go func() {
		sub := &Subscriber{
			ID:     "1",
			Name:   name,
			Chan:   make(chan error),
			Writer: actual,
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(sub))
	}()

	<-syncer

	go func() {
		sub := &Subscriber{
			ID:     "2",
			Name:   name,
			Chan:   make(chan error),
			Writer: actualOther,
		}
		orderActual += "sub-"
		syncer <- 0
		fmt.Println(cast.Sub(sub))
	}()

	<-syncer

	go func() {
		msg := &Msg{Name: name, Reader: strings.NewReader(expected)}
		orderActual += "pub-"
		syncer <- 0
		fmt.Println(cast.Pub(msg))
	}()

	<-syncer

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
