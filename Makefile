BIN=pilreg

.PHONY: build install run test

build:
	go build -o $(BIN) ./cmd/pilreg

install:
	go install ./cmd/pilreg

run:
	go run ./cmd/pilreg $(ARGS)

test: build
	go test ./...
	bash tests/func/test_setup.sh
	bash tests/func/test.sh
