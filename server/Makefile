# Makefile for server

BINARY_NAME = server
BIN_DIR = ../bin
OUTPUT = $(BIN_DIR)/$(BINARY_NAME)

.PHONY: all lint test build clean

all: clean lint build

lint:
	@echo "Running golangci-lint on server..."
	@golangci-lint run .

test:
	@echo "No tests in server directory"

build:
	@echo "Building server binary..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(OUTPUT) .
	@echo "Built: $(OUTPUT)"

clean:
	@echo "Cleaning server artifacts..."
	@rm -f $(OUTPUT)
