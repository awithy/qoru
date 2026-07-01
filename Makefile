.PHONY: build test check race demo-all demo-e2e demo-multihop demo-threehop demo-e2e-encrypted demo-e2e-auto-direct gen-dev-certs diagrams

build:
	@mkdir -p build
	go build -o build/qoru ./cmd/qoru

test:
	go test ./...

check: test build

race:
	go test -race ./internal/client ./internal/server ./internal/e2e ./internal/protocol

demo-all: demo-e2e demo-multihop demo-threehop demo-e2e-auto-direct demo-e2e-encrypted

demo-e2e:
	./dev/e2e-demo.sh

demo-multihop:
	./dev/e2e-multihop.sh

demo-threehop:
	./dev/e2e-threehop.sh

demo-e2e-encrypted:
	./dev/e2e-encrypted-multihop.sh

demo-e2e-auto-direct:
	./dev/e2e-auto-direct.sh

gen-dev-certs:
	./dev/gen-certs.sh

diagrams:
	@for f in docs/diagrams/*.mmd; do \
		out="$${f%.mmd}.png"; \
		npx -y @mermaid-js/mermaid-cli -i "$$f" -o "$$out" -b white; \
	done
