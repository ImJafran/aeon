BINARY_NAME=aeon
BUILD_DIR=bin
VERSION?=0.1.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build run test lint clean docker-build docker-test

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/aeon

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

test:
	CGO_ENABLED=0 go test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)

docker-build:
	docker compose run --rm build

docker-test:
	docker compose run --rm test

docker-run:
	docker compose run --rm --service-ports dev
