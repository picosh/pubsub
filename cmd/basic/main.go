package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/picosh/pubsub"
)

func main() {
	ctx := context.TODO()
	logger := slog.Default()
	broker := pubsub.NewMulticast(logger)

	chann := []*pubsub.Channel{
		pubsub.NewChannel("my-topic"),
	}

	go func() {
		writer := bytes.NewBufferString("my data")
		_ = broker.Pub(ctx, "pubID", writer, chann, false)
	}()

	reader := bytes.NewBufferString("")
	_ = broker.Sub(ctx, "subID", reader, chann, false)

	// result
	fmt.Println("data from pub:", reader)
}
