.PHONY: build test demo-e2e gen-dev-certs

build:
	@mkdir -p build
	go build -o build/qoru ./cmd/qoru

test:
	go test ./...

demo-e2e:
	./dev/e2e-demo.sh

gen-dev-certs:
	./dev/gen-certs.sh
