# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and Development Commands

```bash
# Build everything (clean + lint + build)
make all

# Build binaries only (outputs to bin/)
make build

# Lint with golangci-lint
make lint

# Run all tests
make test

# Run tests directly with Go (unit + integration)
go test ./...

# Run a single test
go test -v -run TestMessageRouting ./test/

# Run only integration tests
go test -v ./test/

# Run only framing unit tests
go test -v ./test/ -run TestFraming

# Regenerate protobuf (requires protoc)
protoc --go_out=. --go_opt=paths=source_relative internal/proto/talkers.proto
```

## Architecture

QUIC-based message broker: clients connect to a central server over QUIC/TLS and exchange protobuf messages through a single bidirectional stream per client.

**Wire protocol**: 4-byte big-endian length prefix + protobuf `Envelope`. The `Envelope` uses a protobuf `oneof` to carry `Register`, `Message`, or `Error` payloads. Framing logic is in `internal/framing/framing.go` with a 512KB max frame size.

**Connection lifecycle**: Client opens a QUIC stream, sends a `Register` envelope (must be first message), then enters a message loop sending `Message` envelopes formatted as `<to_id>:<content>` from stdin. Server validates registration, adds client to the registry, and routes messages by looking up the destination client ID.

**Server structure** (`server/`):
- `main.go` - QUIC listener setup, signal handling, graceful shutdown
- `registry.go` - Thread-safe `map[string]*ClientConn` with RWMutex, 16-client cap
- `handler.go` - Per-connection goroutine: registration, message loop, routing via `routeMessage()`

**Key internal packages**:
- `internal/framing` - `ReadEnvelope`/`WriteEnvelope` used by both client and server
- `internal/errors` - Shared error string constants (used for exact-match assertions in tests)
- `internal/tlsutil` - Generates ephemeral self-signed ECDSA P-256 certs at startup
- `internal/proto` - Generated protobuf code from `talkers.proto`

**Tests** (`test/`): Integration tests duplicate the server's `Registry`, `handleConnection`, and `routeMessage` types/functions locally (not imported from `server/` since it's `package main`). Test helpers `startTestServer`, `connectClient`, `sendMessage`, `expectMessage`, `expectError` set up embedded QUIC servers on random ports.

## Constraints

- Max 16 concurrent clients; client IDs 1-32 chars; message content max 250,000 chars
- Self-signed TLS with `InsecureSkipVerify` on client side (development only)
- QUIC ALPN protocol name is `"talkers"`
- Server uses `log` (stdlib) to stdout; client uses `fmt.Fprintf` to stderr for errors
- Each sub-Makefile uses `golangci-lint run .` (not `go vet`)
