BINARY_NAME=aeon
BUILD_DIR=bin
VERSION?=0.0.1-beta
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

# Install location (no sudo needed)
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin

.PHONY: build build-debug build-linux run test test-verbose lint clean install uninstall docker-build docker-test docker-run

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
	@mkdir -p $(INSTALL_BIN_DIR)
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN_DIR)/$(BINARY_NAME).new
	@chmod +x $(INSTALL_BIN_DIR)/$(BINARY_NAME).new
	@mv -f $(INSTALL_BIN_DIR)/$(BINARY_NAME).new $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Installed $(INSTALL_BIN_DIR)/$(BINARY_NAME)"

uninstall:
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Removed $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "To also remove all data: rm -rf ~/.aeon"
	@echo "Or run: aeon uninstall (removes binary + data + systemd service)"

docker-build:
	docker compose run --rm build

docker-test:
	docker compose run --rm test

docker-run:
	docker compose run --rm --service-ports dev
