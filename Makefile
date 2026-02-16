BINARY := dcc
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X github.com/mochlast/devcontainer-companion/cmd.version=$(VERSION)"

.PHONY: build test lint install clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

lint:
	go vet ./...

install: build
	mkdir -p $(firstword $(if $(GOPATH),$(GOPATH),$(HOME)/go))/bin
	mv $(BINARY) $(firstword $(if $(GOPATH),$(GOPATH),$(HOME)/go))/bin/$(BINARY)

clean:
	rm -f $(BINARY)
