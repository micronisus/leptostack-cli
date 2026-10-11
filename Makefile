VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

PKG := github.com/micronisus/leptostack-cli
LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GO ?= go

.PHONY: all build test test-race vet lint fmt tidy install clean ci release

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/leptostack .

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

install: build
	install -m 0755 bin/leptostack /usr/local/bin/leptostack

clean:
	rm -rf bin dist

# ci and release run the exact scripts the GitHub and Forgejo workflows call,
# so local runs match CI on either forge.
ci:
	bash scripts/ci.sh

release:
	bash scripts/release.sh
