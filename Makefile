.PHONY: build test coverage fmt lint proto-gen ci benchmark docker-build cd clean examples-build examples-tidy examples-fmt-check

BINARY   := token-engine
PKG      := ./...
TEST_PKG := ./internal/... ./client/... ./integration/
IMAGE    := angeltomala/token-engine
VERSION  := $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")
DOCKER   := podman

# Every examples/ subdirectory with a go.mod — each example is an independent Go module.
EXAMPLE_DIRS := $(patsubst %/go.mod,%,$(wildcard examples/*/go.mod))

build:
	go build -o $(BINARY) ./cmd/token-engine

test:
	ginkgo -r --race $(TEST_PKG)

coverage:
	ginkgo -r --race --cover --coverprofile=coverage.out $(TEST_PKG)
	go tool cover -html=coverage.out -o coverage.html

fmt:
	gofmt -w .

lint:
	go vet $(PKG)
	golangci-lint run $(PKG)
	./scripts/govulncheck-gate.sh

proto-gen:
	buf generate

ci: lint build test examples-build examples-fmt-check

benchmark:
	go test -bench=. -benchmem -run=^$$ -count=5 -timeout=30m ./integration/bench/

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
	@for d in $(EXAMPLE_DIRS); do \
		echo "==> building $$d"; \
		(cd $$d && go build ./...) || exit 1; \
	done

examples-tidy:
	@for d in $(EXAMPLE_DIRS); do \
		echo "==> tidying $$d"; \
		(cd $$d && go mod tidy) || exit 1; \
	done

# golangci-lint's gofmt formatter cannot see examples/ (separate modules); gofmt -l is module-agnostic.
examples-fmt-check:
	@unformatted="$$(gofmt -l examples)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed in:"; echo "$$unformatted"; exit 1; \
	fi
