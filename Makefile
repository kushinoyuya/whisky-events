.PHONY: all build test vet fmt cover ping sample crawl clean

all: vet test build

build:
	go build -o bin/whisky-events ./cmd/whisky-events

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

# Requires `ollama serve` running and the model pulled.
ping:
	go run ./cmd/whisky-events ping

sample:
	go run ./cmd/whisky-events extract -file testdata/sample_event.html

# Example: make crawl ARGS="-site 福岡"
crawl:
	go run ./cmd/whisky-events crawl $(ARGS)

clean:
	rm -rf bin coverage.out
