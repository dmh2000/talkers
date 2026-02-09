# Top-level Makefile for talkers project

# Subdirectories with Makefiles
SUBDIRS = client server internal/proto internal/framing internal/tlsutil internal/errors test

.PHONY: all lint test build clean $(SUBDIRS)

# Default target
all: clean lint build

# Run all subdirectory targets
lint test build clean: $(SUBDIRS)

# Call each subdirectory's Makefile
$(SUBDIRS):
	@echo "===> $@ ($@)"
	@$(MAKE) -C $@ $(MAKECMDGOALS)

# Help target
help:
	@echo "Talkers Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  all     - Run clean, lint, and build (default)"
	@echo "  lint    - Run golangci-lint on all packages"
	@echo "  test    - Run tests in all packages"
	@echo "  build   - Build all binaries"
	@echo "  clean   - Remove all build artifacts"
	@echo "  help    - Show this help message"
	@echo ""
	@echo "Subdirectories: $(SUBDIRS)"
