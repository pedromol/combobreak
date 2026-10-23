.PHONY: all build test run clean

BINARY_NAME=combobreak

all: test build

build:
	go build -o $(BINARY_NAME) .

run:
	go run main.go

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)
