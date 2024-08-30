FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

ENV CGO_ENABLED=0

WORKDIR /usr/src/app

COPY go.* .

RUN --mount=type=cache,target=/go/pkg/,rw \
  --mount=type=cache,target=/root/.cache/,rw \
  go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

ENV GOOS=${TARGETOS} GOARCH=${TARGETARCH}

RUN --mount=type=cache,target=/go/pkg/,rw \
  --mount=type=cache,target=/root/.cache/,rw \
  go build -o pubsub ./cmd/authorized_keys

FROM alpine

RUN apk add --no-cache curl

COPY --from=builder /usr/src/app/pubsub /pubsub

ENTRYPOINT [ "/pubsub" ]
