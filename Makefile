BINARY_NAME=aeon
BUILD_DIR=bin
VERSION?=0.1.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build build-debug run test test-verbose lint clean docker-build docker-test install

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/aeon

build-debug:
	CGO_ENABLED=0 go build -o $(BUILD_DIR)/$(BINARY_NAME)-debug ./cmd/aeon

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/aeon
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/aeon

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

test:
	CGO_ENABLED=0 go test ./... -count=1

test-verbose:
	CGO_ENABLED=0 go test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)

install: build
	cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)

docker-build:
	docker compose run --rm build

docker-test:
	docker compose run --rm test

docker-run:
	docker compose run --rm --service-ports dev
