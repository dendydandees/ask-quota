# CGO_ENABLED=0 on every build target. A native build defaults to cgo on, which
# links the host libc and produces a dynamic binary — so the amd64 release was
# dynamic while the cross-compiled arm64 one was static, and the two could not
# be reproduced from the same source on a different machine.
#
# Stripped of anything that is not tag-shaped: the value is interpolated into
# -ldflags, where a space or a quote in a tag name would inject linker flags.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | tr -cd 'A-Za-z0-9.+_-' || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint install man dist clean audit live-update

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ask-quota ./cmd/ask-quota

test:
	go test -race ./...

live-update:
	go test -tags=live ./internal/quota/ -run TestLiveGolden -update

audit:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }

install: build
	install -Dm755 dist/ask-quota $(HOME)/.local/bin/ask-quota
	install -Dm644 man/ask-quota.1 $(HOME)/.local/share/man/man1/ask-quota.1

man:
	man ./man/ask-quota.1

# dist builds the release artefacts and the checksums that go beside them: with
# no dependencies, the published binary is the entire supply chain.
dist: clean
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ask-quota-linux-amd64 ./cmd/ask-quota
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/ask-quota-linux-arm64 ./cmd/ask-quota
	cd dist && sha256sum ask-quota-* > checksums.txt

clean:
	rm -rf dist
