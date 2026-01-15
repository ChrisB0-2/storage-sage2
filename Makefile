# StorageSage Makefile
# Provides CI parity for local development

VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -s -w -X main.version=$(VERSION)

BINARY  := storage-sage
CMD     := ./cmd/storage-sage

# Build targets
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build clean test lint vet fmt tidy help
.PHONY: build-all $(PLATFORMS)

all: tidy fmt vet lint test build

# Local build
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)

# Run all tests with race detector
test:
	go test -race -cover ./...

# Run tests with coverage report
test-coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run go vet
vet:
	go vet ./...

# Run gofmt
fmt:
	gofmt -s -w .

# Check formatting (CI mode)
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

# Run golangci-lint
lint:
	golangci-lint run

# Tidy and verify go.mod
tidy:
	go mod tidy
	go mod verify

# Clean build artifacts
clean:
	rm -f $(BINARY) storage-sage-* coverage.out coverage.html
	rm -rf dist/

# Cross-platform builds
build-all: $(PLATFORMS)

$(PLATFORMS):
	$(eval GOOS := $(word 1,$(subst /, ,$@)))
	$(eval GOARCH := $(word 2,$(subst /, ,$@)))
	$(eval EXT := $(if $(filter windows,$(GOOS)),.exe,))
	@echo "Building $(GOOS)/$(GOARCH)..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags="$(LDFLAGS)" \
		-o dist/$(BINARY)-$(GOOS)-$(GOARCH)$(EXT) $(CMD)

# Generate checksums for release
checksums:
	@cd dist && sha256sum $(BINARY)-* > SHA256SUMS

# Full release build
release: clean build-all checksums
	@echo "Release artifacts in dist/"
	@ls -la dist/

# Install locally
install: build
	mv $(BINARY) $(GOPATH)/bin/$(BINARY) 2>/dev/null || mv $(BINARY) ~/go/bin/$(BINARY)

# Run the binary (dry-run mode)
run: build
	./$(BINARY) -mode dry-run

# CI target - matches GitHub Actions workflow
ci: tidy fmt-check vet lint test build

help:
	@echo "StorageSage Build Targets:"
	@echo ""
	@echo "  make              - Run full CI pipeline locally (tidy, fmt, vet, lint, test, build)"
	@echo "  make build        - Build binary for current platform"
	@echo "  make test         - Run tests with race detector"
	@echo "  make test-coverage- Run tests with coverage report"
	@echo "  make lint         - Run golangci-lint"
	@echo "  make vet          - Run go vet"
	@echo "  make fmt          - Format code with gofmt"
	@echo "  make tidy         - Tidy go.mod"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make build-all    - Build for all platforms"
	@echo "  make release      - Full release build with checksums"
	@echo "  make ci           - Run CI checks (matches GitHub Actions)"
	@echo "  make install      - Install binary to GOPATH/bin"
	@echo "  make run          - Build and run in dry-run mode"
	@echo ""
	@echo "Platforms: $(PLATFORMS)"
