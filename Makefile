BINARY := envault
PKG := ./cmd/envault
DIST := dist

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# os/arch pairs shipped in a release.
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build run test clean release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

run:
	go run $(PKG)

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)

# release cross-compiles every platform into dist/ and writes SHA256 checksums.
# Runs unchanged on macOS (shasum) and Linux/CI (sha256sum).
release:
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out=$(DIST)/$(BINARY)_$${os}_$${arch}; \
		[ "$$os" = "windows" ] && out=$$out.exe || true; \
		echo "building $$out ($(VERSION))"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out $(PKG) || exit 1; \
	done
	@cd $(DIST) && { \
		if command -v sha256sum >/dev/null 2>&1; then sha256sum $(BINARY)_*; \
		elif command -v shasum >/dev/null 2>&1; then shasum -a 256 $(BINARY)_*; \
		else echo "no sha256 tool found" >&2; exit 1; fi; \
	} > checksums.txt
	@echo "--- $(DIST)/ ---" && ls -la $(DIST) && echo "--- checksums ---" && cat $(DIST)/checksums.txt
