VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint install man clean

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ask-quota ./cmd/ask-quota

test:
	go test ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }

install: build
	install -Dm755 dist/ask-quota $(HOME)/.local/bin/ask-quota
	install -Dm644 man/ask-quota.1 $(HOME)/.local/share/man/man1/ask-quota.1

man:
	man ./man/ask-quota.1

clean:
	rm -rf dist
