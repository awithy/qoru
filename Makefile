.PHONY: build test

build:
	@mkdir -p build
	go build -o build/gorelay ./cmd/gorelay

test:
	go test ./...
