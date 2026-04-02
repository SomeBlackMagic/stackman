BINARY_NAME=stackman
VERSION?=dev
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)"

.PHONY: build test test-integration test-integration-full swarm-init swarm-leave lint clean docker-build help

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) .

test:
	go test -race ./...

test-integration:
	@echo "Running integration tests..."
	@echo "NOTE: This requires Docker Swarm to be initialized"
	go test -v -race -tags=integration -timeout=5m ./tests/...

test-integration-full: swarm-init test-integration

swarm-init:
	@echo "Checking Docker Swarm status..."
	@docker info | grep -q "Swarm: active" || docker swarm init

swarm-leave:
	@echo "Leaving Docker Swarm..."
	@docker swarm leave --force || true

lint:
	go vet ./...

clean:
	rm -f $(BINARY_NAME)
	@docker stack ls | grep stackman-test | awk '{print $$1}' | xargs -r docker stack rm || true

docker-build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t stackman:$(VERSION) .

help:
	@echo "Available targets:"
	@echo "  build                  - Build stackman binary"
	@echo "  test                   - Run unit tests"
	@echo "  test-integration       - Run integration tests (requires Swarm)"
	@echo "  test-integration-full  - Initialize Swarm and run integration tests"
	@echo "  swarm-init             - Initialize Docker Swarm"
	@echo "  swarm-leave            - Leave Docker Swarm"
	@echo "  lint                   - Run go vet"
	@echo "  clean                  - Clean build artifacts and test stacks"
	@echo "  docker-build           - Build multiarch docker image"
	@echo "  help                   - Show this help message"
