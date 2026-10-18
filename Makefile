# Makefile for the ytd CLI and repository tooling.
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
PKG := ./cmd/ytd
BIN := bin/ytd
ifeq ($(OS),Windows_NT)
BIN := bin/ytd.exe
endif

CROSS_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: build build-all test test-race vet lint fmt checklines check clean

build:
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(BIN) $(PKG)

build-all:
	@mkdir -p bin
	@set -e; for t in $(CROSS_TARGETS); do \
		goos=$${t%/*}; goarch=$${t#*/}; ext=""; \
		if [ "$$goos" = windows ]; then ext=".exe"; fi; \
		echo "building $$goos/$$goarch"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
			go build -o bin/ytd-$$goos-$$goarch$$ext $(PKG); \
	done

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

checklines:
	go run ./scripts/checklines

check:
	@files="$$(gofmt -l .)"; if [ -n "$$files" ]; then \
		echo "gofmt reports unformatted files:"; echo "$$files"; exit 1; \
	fi
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) checklines

clean:
	rm -rf bin
