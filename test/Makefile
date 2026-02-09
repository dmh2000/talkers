# Makefile for test

.PHONY: all lint test build clean

all: clean lint test

lint:
	@echo "Running golangci-lint on test..."
	@golangci-lint run .

test:
	@echo "Running tests..."
	@go test -v .

build:
	@echo "No build required for test directory (test package)"

clean:
	@echo "Cleaning test artifacts..."
	@go clean -testcache
	@echo "Test cache cleared"
