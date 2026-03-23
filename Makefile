BINARY := idea-engine
PKG := github.com/timholm/idea-engine
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build test clean lint run

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./... -v -race -count=1

clean:
	rm -f $(BINARY)
	go clean -cache -testcache

lint:
	go vet ./...

run: build
	./$(BINARY) run

install:
	go install $(LDFLAGS) .
