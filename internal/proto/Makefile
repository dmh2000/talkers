# Makefile for internal/proto

PROTO_SRC = talkers.proto
PROTO_OUT = talkers.pb.go

.PHONY: all lint test build clean

all: clean lint build

lint:
	@echo "Running golangci-lint on internal/proto..."
	@golangci-lint run .

test:
	@echo "No tests in internal/proto directory"

build:
	@if [ $(PROTO_SRC) -nt $(PROTO_OUT) ]; then \
		echo "Rebuilding $(PROTO_OUT) from $(PROTO_SRC)..."; \
		protoc --go_out=. --go_opt=paths=source_relative $(PROTO_SRC); \
		echo "Generated: $(PROTO_OUT)"; \
	else \
		echo "$(PROTO_OUT) is up to date"; \
	fi

clean:
	@echo "Cleaning internal/proto artifacts..."
	@echo "Note: talkers.pb.go is generated from talkers.proto and not removed by clean"
	@echo "      Run 'rm talkers.pb.go' manually if needed, then run 'make build' to regenerate"
