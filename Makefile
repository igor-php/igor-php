.PHONY: build test lint clean

# Build the main binary
build:
	go build -o bin/igor ./cmd/igor/...

# Run all tests
test:
	go test ./... -v

# Run the linter
lint:
	golangci-lint run ./...

# Clean the build directory
clean:
	rm -rf bin/
