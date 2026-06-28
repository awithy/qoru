.PHONY: build test gen-dev-certs

build:
	@mkdir -p build
	go build -o build/qoru ./cmd/qoru

test:
	go test ./...

gen-dev-certs:
	./dev/gen-certs.sh
