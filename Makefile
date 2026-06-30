.PHONY: build test demo-e2e demo-multihop demo-threehop gen-dev-certs

build:
	@mkdir -p build
	go build -o build/qoru ./cmd/qoru

test:
	go test ./...

demo-e2e:
	./dev/e2e-demo.sh

demo-multihop:
	./dev/e2e-multihop.sh

demo-threehop:
	./dev/e2e-threehop.sh

gen-dev-certs:
	./dev/gen-certs.sh
