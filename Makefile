.PHONY: lint test test-cover build check clean

# Default: full check pipeline
check: lint test build

# Build the main binary
build:
	go build -o bin/ngoagent ./cmd/ngoagent

# Run tests with race detector
test:
	go test -race -count=1 -timeout 120s ./internal/...

# Run tests with coverage report
test-cover:
	go test -race -coverprofile=cover.out -timeout 120s ./internal/...
	go tool cover -func=cover.out | tail -1
	@echo "Full report: go tool cover -html=cover.out"

# Lint with golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found, skipping lint"; exit 0; }
	golangci-lint run --timeout 5m ./...

# File size guardian: warn if any non-generated Go file exceeds 500 lines
size-check:
	@echo "=== Files exceeding 500 lines ==="
	@find internal/ -name '*.go' ! -name '*_test.go' ! -name '*.pb.go' \
		-exec awk 'END{if(NR>500) printf "  ⚠️  %s: %d lines\n", FILENAME, NR}' {} \;
	@echo "=== Done ==="

# Clean build artifacts
clean:
	rm -rf bin/ cover.out
