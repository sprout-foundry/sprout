.PHONY: build test test-e2e vet fmt check

# Build all packages
build:
	go build ./...

# Run all tests
test:
	go test ./...

# Run e2e tests with verbose output
test-e2e:
	go test -v ./internal/test/...

# Run go vet
vet:
	go vet ./...

# Format code
fmt:
	go fmt ./...

# Run conformance tests
conformance: build
	go build -o seed-cli ./cmd/seed-cli/
	go run ./conformance/runner/ --cli ./seed-cli --specs ./conformance/specs/

# Full check: vet, fmt, build, test
check: vet fmt build test
