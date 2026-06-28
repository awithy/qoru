.PHONY: build test gen-dev-certs

build:
	@mkdir -p build
	go build -o build/gorelay ./cmd/gorelay

test:
	go test ./...

gen-dev-certs:
	./dev/gen-certs.sh
