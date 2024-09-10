fmt:
	go fmt ./...
.PHONY: fmt

lint:
	golangci-lint run -E goimports -E godot --timeout 10m
.PHONY: lint

test:
	go test -v ./... -cover -race -coverprofile=coverage.out
	go tool cover -func=coverage.out -o=coverage.out
.PHONY: test
