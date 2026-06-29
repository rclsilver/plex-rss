.PHONY: build clean test run fmt vet tidy deps install version help

# Variables
BINARY_NAME=plex-rss
MAIN_PATH=./cmd/plex-rss
GO=go
GOFLAGS=-v

# Version management
MAIN_PKG    = github.com/rclsilver/plex-rss
VERSION_PKG = ${MAIN_PKG}/internal/version
VERSION     ?= $(shell ./generate-version.sh)
LAST_COMMIT = $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

# Build flags
LD_FLAGS = -ldflags "-w -s -X ${VERSION_PKG}.version=${VERSION} -X ${VERSION_PKG}.commit=${LAST_COMMIT}"

# Default target
.DEFAULT_GOAL := help

## build: Compile the binary
build:
	@echo "Building $(BINARY_NAME) version $(VERSION)..."
	$(GO) build $(GOFLAGS) $(LD_FLAGS) -o $(BINARY_NAME) $(MAIN_PATH)

## clean: Clean generated files
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME)
	@$(GO) clean

## test: Run tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

## run: Build and run the application
run: build
	@echo "Running $(BINARY_NAME)..."
	./$(BINARY_NAME)

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

## vet: Analyze code
vet:
	@echo "Vetting code..."
	$(GO) vet ./...

## tidy: Clean dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GO) mod tidy

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

## install: Install the binary
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GO) install $(LD_FLAGS) $(MAIN_PATH)

## version: Show version information
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(LAST_COMMIT)"

## help: Show this help
help: Makefile
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $< | column -t -s ':' | sed -e 's/^/ /'
