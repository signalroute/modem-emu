.PHONY: build test lint docker clean

BINARY = bin/modem-emu

build:
	mkdir -p bin && go build -ldflags="-s -w" -o $(BINARY) ./cmd/emu

test:
	go test -count=1 -timeout 120s -race ./...

lint:
	golangci-lint run ./...

docker:
	docker build -t modem-emu .

clean:
	rm -rf bin
