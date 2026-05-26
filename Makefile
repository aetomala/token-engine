.PHONY: build test coverage lint proto-gen ci clean

BINARY   := token-engine
PKG      := ./...
TEST_PKG := ./internal/...

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

clean:
	go clean $(PKG)
	find . -name "cover*.out" -delete
	rm -f coverage.html $(BINARY)
