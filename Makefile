.PHONY: all build test run clean ko-build

BINARY_NAME=combobreak

all: test build

build:
	go build -o $(BINARY_NAME) .

ko-build:
	ko build --bare --platform=linux/amd64,linux/arm64 --push=false .

run:
	go run main.go

test:
	go test -v ./...

clean:
	rm -f $(BINARY_NAME)
