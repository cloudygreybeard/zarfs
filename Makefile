# zarfs Makefile

PROJECT_ROOT := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
OUTPUT_DIR := $(PROJECT_ROOT)/_output
BINARY := $(OUTPUT_DIR)/bin/zarfs
PREFIX ?= /usr/local
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/cloudygreybeard/zarfs/cmd.Version=$(VERSION) \
	-X github.com/cloudygreybeard/zarfs/cmd.Commit=$(COMMIT) \
	-X github.com/cloudygreybeard/zarfs/cmd.Date=$(DATE)

.PHONY: all build build-all test lint clean install snapshot deps help

## all: Build the binary (default target)
all: build

## build: Build the binary for the current platform
build:
	@mkdir -p $(OUTPUT_DIR)/bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## build-all: Cross-compile for all release platforms
build-all:
	@mkdir -p $(OUTPUT_DIR)/bin
	GOOS=linux  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/bin/zarfs-linux-amd64 .
	GOOS=linux  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/bin/zarfs-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/bin/zarfs-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/bin/zarfs-darwin-arm64 .

## test: Run tests
test:
	go test -v -race ./...

## lint: Run linter
lint:
	golangci-lint run

## clean: Remove build artifacts
clean:
	rm -rf $(OUTPUT_DIR)
	rm -rf dist/

## install: Install to PREFIX/bin
install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/zarfs

## snapshot: Build a snapshot release (no publish)
snapshot:
	goreleaser release --snapshot --clean

## deps: Download dependencies
deps:
	go mod download
	go mod tidy

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
