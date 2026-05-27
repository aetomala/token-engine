#!/usr/bin/env bash
# Reproduces the full CI pipeline locally.
# Exit code: 0 if all steps pass, 1 if any step fails.
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FAILURES=0

run_step() {
    local name="$1"
    shift
    echo -e "${YELLOW}>>> ${name}${NC}"
    if "$@"; then
        echo -e "${GREEN}PASS: ${name}${NC}\n"
    else
        echo -e "${RED}FAIL: ${name}${NC}\n"
        FAILURES=$((FAILURES + 1))
    fi
}

run_step "go vet"         go vet ./...
run_step "golangci-lint"  golangci-lint run ./...
run_step "govulncheck"    govulncheck ./...
run_step "go build"       go build ./...
run_step "buf lint"       buf lint
run_step "tests (race)"   ginkgo -r --race ./internal/...

if [ "$FAILURES" -gt 0 ]; then
    echo -e "${RED}${FAILURES} step(s) failed.${NC}"
    exit 1
fi

echo -e "${GREEN}All steps passed.${NC}"
