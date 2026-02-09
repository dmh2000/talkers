# Makefile for client

BINARY_NAME = client
BIN_DIR = ../bin
OUTPUT = $(BIN_DIR)/$(BINARY_NAME)

.PHONY: all lint test build clean

all: clean lint build

lint:
	@echo "Running golangci-lint on client..."
	@golangci-lint run .

test:
	@echo "No tests in client directory"

build:
	@echo "Building client binary..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(OUTPUT) .
	@echo "Built: $(OUTPUT)"

clean:
	@echo "Cleaning client artifacts..."
	@rm -f $(OUTPUT)
