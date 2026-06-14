GO ?= go
BIN := chronicle

.PHONY: build test vet tidy run clean

build:
	$(GO) build -o $(BIN) ./cmd/chronicle

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

run:
	$(GO) run ./cmd/chronicle

clean:
	rm -f $(BIN)
