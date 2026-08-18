BINARY := ltop
PREFIX ?= $(HOME)/.local

.PHONY: build test vet fmt run once install clean

build:
	go build -o $(BINARY) .

test:
	go test ./...

vet:
	gofmt -l . && go vet ./...

fmt:
	gofmt -w .

run: build
	./$(BINARY)

once: build
	./$(BINARY) -once

install: build
	install -Dm755 $(BINARY) $(PREFIX)/bin/$(BINARY)

clean:
	rm -f $(BINARY)
