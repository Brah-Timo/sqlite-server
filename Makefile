# ============================================================
#  sqlite-server — Build System
# ============================================================

BINARY      := sqlite-server
MODULE      := github.com/sqlite-server/sqlite-server
CMD_PKG     := $(MODULE)/cmd/sqlite-server
BUILD_DIR   := bin
DIST_DIR    := dist

# Version is the current git tag (or commit hash if no tag).
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
	-X main.Version=$(VERSION) \
	-X main.Commit=$(COMMIT) \
	-X main.BuildDate=$(BUILD_DATE)

GO_FLAGS := -trimpath -ldflags "$(LDFLAGS)"

# ── Default ───────────────────────────────────────────────────────────────────
.DEFAULT_GOAL := build

.PHONY: all build build-all install clean test test-unit test-integration \
        lint fmt vet tidy docker docker-push release help

# ── Build (current platform) ──────────────────────────────────────────────────
build:
	@echo "→ building $(BINARY) $(VERSION) for $(shell go env GOOS)/$(shell go env GOARCH)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_PKG)
	@echo "✓ $(BUILD_DIR)/$(BINARY)"

# ── Build all platforms ────────────────────────────────────────────────────────
build-all:
	@echo "→ cross-compiling for all platforms…"
	@mkdir -p $(DIST_DIR)

	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-linux-amd64   $(CMD_PKG)

	GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-linux-arm64   $(CMD_PKG)

	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-darwin-amd64  $(CMD_PKG)

	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-darwin-arm64  $(CMD_PKG)

	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-windows-amd64.exe $(CMD_PKG)

	GOOS=freebsd GOARCH=amd64  CGO_ENABLED=0 go build $(GO_FLAGS) \
		-o $(DIST_DIR)/$(BINARY)-freebsd-amd64 $(CMD_PKG)

	@echo "✓ binaries written to $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

# ── Install ────────────────────────────────────────────────────────────────────
install: build
	@cp $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "✓ installed /usr/local/bin/$(BINARY)"

# ── Tests ──────────────────────────────────────────────────────────────────────
test: test-unit

test-unit:
	@echo "→ unit tests"
	go test ./... -v -race -timeout 60s \
		-coverprofile=coverage.out -covermode=atomic
	@echo "✓ done"

test-integration:
	@echo "→ integration tests (requires running sqlite-server on :5432)"
	go test ./tests/integration/... -v -race -timeout 120s

coverage: test-unit
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ coverage report: coverage.html"

# ── Code Quality ──────────────────────────────────────────────────────────────
lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		{ echo "installing golangci-lint…"; \
		  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; }
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	@echo "✓ formatted"

vet:
	go vet ./...
	@echo "✓ vet clean"

tidy:
	go mod tidy
	@echo "✓ modules tidied"

# ── Docker ────────────────────────────────────────────────────────────────────
DOCKER_IMAGE := sqlite-server
DOCKER_TAG   := $(VERSION)

docker:
	@echo "→ building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)"
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):latest \
		.
	@echo "✓ docker image: $(DOCKER_IMAGE):$(DOCKER_TAG)"

docker-push: docker
	docker push $(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_IMAGE):latest

# ── Release ───────────────────────────────────────────────────────────────────
release: build-all
	@echo "→ packaging release $(VERSION)"
	@mkdir -p $(DIST_DIR)/release
	@for f in $(DIST_DIR)/$(BINARY)-*; do \
		base=$$(basename $$f); \
		if echo "$$base" | grep -q ".exe"; then \
			zip -j $(DIST_DIR)/release/$${base%.exe}-$(VERSION).zip $$f README.md; \
		else \
			tar -czf $(DIST_DIR)/release/$$base-$(VERSION).tar.gz \
				-C $(DIST_DIR) $$(basename $$f) \
				-C $(CURDIR) README.md; \
		fi; \
	done
	@echo "✓ release archives written to $(DIST_DIR)/release/"
	@ls -lh $(DIST_DIR)/release/

# ── Clean ─────────────────────────────────────────────────────────────────────
clean:
	@rm -rf $(BUILD_DIR) $(DIST_DIR) coverage.out coverage.html
	@echo "✓ clean"

# ── Help ──────────────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "  sqlite-server build system"
	@echo ""
	@echo "  Targets:"
	@echo "    build            Build for the current platform"
	@echo "    build-all        Cross-compile for Linux/macOS/Windows/FreeBSD"
	@echo "    install          Build and install to /usr/local/bin"
	@echo "    test             Run unit tests"
	@echo "    test-integration Run integration tests (needs running server)"
	@echo "    coverage         Generate HTML coverage report"
	@echo "    lint             Run golangci-lint"
	@echo "    fmt              Format all Go code"
	@echo "    vet              Run go vet"
	@echo "    tidy             go mod tidy"
	@echo "    docker           Build Docker image"
	@echo "    docker-push      Push Docker image to registry"
	@echo "    release          Package all binaries for release"
	@echo "    clean            Remove build artifacts"
	@echo ""
