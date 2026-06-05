BINARY := envault
PKG := ./cmd/envault

.PHONY: build run clean

build:
	CGO_ENABLED=0 go build -ldflags "-s -w" -o $(BINARY) $(PKG)

run:
	go run $(PKG)

clean:
	rm -f $(BINARY)
