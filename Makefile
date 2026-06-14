.PHONY: build test coverage lint proto-gen ci docker-build cd clean examples-build examples-tidy

BINARY   := token-engine
PKG      := ./...
TEST_PKG := ./internal/... ./client/...
IMAGE    := angeltomala/token-engine
VERSION  := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
DOCKER   := podman

build:
	go build -o $(BINARY) ./cmd/token-engine

test:
	ginkgo -r --race $(TEST_PKG)

coverage:
	ginkgo -r --race --cover --coverprofile=coverage.out $(TEST_PKG)
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet $(PKG)
	golangci-lint run $(PKG)

proto-gen:
	buf generate

ci: lint build test

docker-build:
	$(DOCKER) build -t $(IMAGE):$(VERSION) .

cd:
	@test "$(VERSION)" != "dev" || (echo "error: not on a version tag — run: git checkout v<x.y.z>"; exit 1)
	$(DOCKER) buildx build \
		--platform linux/amd64,linux/arm64 \
		--tag $(IMAGE):$(VERSION) \
		--push \
		.

clean:
	go clean $(PKG)
	find . -name "cover*.out" -delete
	rm -f coverage.html $(BINARY)

examples-build:
	@for d in examples/grpc-client examples/mtls-client examples/custom-claims examples/multi-tenant; do \
		echo "==> building $$d"; \
		(cd $$d && go build ./...); \
	done

examples-tidy:
	@for d in examples/grpc-client examples/mtls-client examples/custom-claims examples/multi-tenant; do \
		echo "==> tidying $$d"; \
		(cd $$d && go mod tidy); \
	done
