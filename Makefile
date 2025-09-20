.PHONY: all build install test benchmark clean version release release-snapshot

GO ?= go
PKG := github.com/AlexanderGrooff/spage
BINARY := spage
BIN_DIR := dist

GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/cmd.Version=$(VERSION) -X $(PKG)/cmd.GitCommit=$(GIT_COMMIT) -X $(PKG)/cmd.BuildDate=$(BUILD_DATE)

all: build

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) .

install:
	$(GO) install -ldflags "$(LDFLAGS)" .

test:
	$(GO) test -v -race -cover -count=1 ./...

benchmark:
	$(GO) test -run=^$$ -bench=. -benchmem -count=3 ./...

clean:
	rm -rf $(BIN_DIR)

version:
	@echo "Version:     $(VERSION)"
	@echo "Git commit:  $(GIT_COMMIT)"
	@echo "Build date:  $(BUILD_DATE)"

release:
	@if [ -z "$(TAG)" ]; then \
		echo "Error: TAG is required. Usage: make release TAG=v1.0.0"; \
		exit 1; \
	fi
	@echo "Creating release $(TAG)..."
	git tag $(TAG)
	git push origin $(TAG)
	@echo "Release $(TAG) created and pushed. GitHub Actions will handle the build and release."

release-snapshot:
	@echo "Building release snapshot with goreleaser..."
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required but not installed. Install it from https://goreleaser.com/install/"; exit 1; }
	goreleaser release --snapshot --clean
	@echo "Snapshot release built in dist/ directory"
