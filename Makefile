BIN=pilreg
BUILDDATE=$(shell date +"%Y-%m-%d %H:%M:%S")
LDFLAGS=-ldflags "-X 'main.version=2.0' -X 'main.buildDate=$(BUILDDATE)'"

.PHONY: build install run test

build:
	go build $(LDFLAGS) -o $(BIN) ./cmd/pilreg

install:
	go install $(LDFLAGS) ./cmd/pilreg

run:
	go run $(LDFLAGS) ./cmd/pilreg $(ARGS)

test: build
	go test ./...
	bash tests/func/test_setup.sh
	bash tests/func/test.sh

