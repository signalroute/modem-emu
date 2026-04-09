BINARY  := go-modem-emu
CMD     := ./cmd/emu
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test test-race test-verbose tidy clean help

all: tidy build

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)
	@echo "Built $(BINARY)"

test:
	CGO_ENABLED=0 go test ./... -count=1

test-verbose:
	CGO_ENABLED=0 go test ./... -v -count=1

test-race:
	CGO_ENABLED=0 go test ./... -race -count=1 -timeout 60s

test-run:
	CGO_ENABLED=0 go test ./... -run $(T) -v -count=1

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

help:
	@echo "go-modem-emu build targets"
	@echo "  make build         Build binary"
	@echo "  make test          Run all tests"
	@echo "  make test-race     Tests with race detector"
	@echo "  make test-run T=X  Run tests matching pattern X"
