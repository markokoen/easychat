APP_NAME=easychat

.PHONY: run build test fmt tidy

run:
	go run ./cmd/server

build:
	go build ./...

test:
	go test ./...

fmt:
	gofmt -w $$(find . -type f -name '*.go' -not -path './vendor/*')

tidy:
	go mod tidy
