# Talkers — Implementation Plan

## Prerequisites

- Go 1.21+ installed
- `protoc` (Protocol Buffers compiler) installed
- `protoc-gen-go` plugin installed (`go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`)

---

## Step 1: Project Initialization

**Goal**: Set up the Go module and directory structure.

1. Initialize the Go module: `go mod init talkers`
2. Create directories:
   - `client/`
   - `server/`
   - `internal/proto/` — generated protobuf code
   - `internal/tlsutil/` — self-signed certificate generation
   - `internal/framing/` — length-prefixed read/write helpers
   - `internal/errors/` — shared error string constants
   - `test/`
3. Install dependencies:
   - `go get github.com/quic-go/quic-go`
   - `go get google.golang.org/protobuf`

---

## Step 2: Protobuf Definition & Code Generation

**Goal**: Define the wire protocol and generate Go types.

1. Create `internal/proto/talkers.proto` with the `Envelope`, `Register`, `Error`, and `Message` definitions from the specification (Section 4.1).
2. Run `protoc` to generate `internal/proto/talkers.pb.go`.
3. Verify the generated code compiles: `go build ./internal/proto/...`

**Files created**:
- `internal/proto/talkers.proto`
- `internal/proto/talkers.pb.go` (generated)

---

## Step 3: Shared Error Constants

**Goal**: Define all error strings in one place so server and client can reference them consistently.

1. Create `internal/errors/errors.go` with exported `const` values:
   - `ErrContentTooLarge` — "content exceeds 250000 character limit"
   - `ErrClientNotRegistered` — "destination client is not registered"
   - `ErrDuplicateClientID` — "client ID is already registered"
   - `ErrMaxClientsReached` — "maximum number of clients (16) reached"
   - `ErrClientDisconnected` — "destination client is disconnected"
   - `ErrUnexpectedMessage` — "unexpected message type after registration"
   - `ErrInvalidFirstMessage` — "first message must be REGISTER"

**Files created**:
- `internal/errors/errors.go`

---

## Step 4: Wire Framing (Length-Delimited I/O)

**Goal**: Implement helpers to write and read length-prefixed protobuf envelopes on a QUIC stream.

1. Create `internal/framing/framing.go` with two functions:
   - `WriteEnvelope(stream quic.Stream, env *proto.Envelope) error` — serialize the envelope, write 4-byte big-endian length prefix, then the payload bytes.
   - `ReadEnvelope(stream quic.Stream) (*proto.Envelope, error)` — read 4-byte length, validate it against a max frame size constant, allocate buffer, read payload, unmarshal.
2. Define `MaxFrameSize` constant (e.g., 512 KB to accommodate 250K character content + protobuf overhead).
3. Write unit tests in `test/framing_test.go`:
   - Round-trip: write then read an envelope of each payload type.
   - Oversized frame rejection.

**Files created**:
- `internal/framing/framing.go`
- `test/framing_test.go`

---

## Step 5: TLS Certificate Generation

**Goal**: Generate an in-memory self-signed certificate for the QUIC server.

1. Create `internal/tlsutil/cert.go` with a function:
   - `GenerateSelfSignedCert() (tls.Certificate, error)` — generates an RSA or ECDSA key pair, creates an X.509 certificate with CN/SAN `sqirvy.xyz`, returns a `tls.Certificate` suitable for `tls.Config`.
2. Write a unit test in `test/tlsutil_test.go`:
   - Verify the returned certificate is valid and contains the expected SAN.

**Files created**:
- `internal/tlsutil/cert.go`
- `test/tlsutil_test.go`

---

## Step 6: Server — Core Implementation

**Goal**: Build the server's main loop, client registry, and message routing.

### 6a: Client Registry

1. Create `server/registry.go` with:
   - `ClientConn` struct wrapping `quic.Connection` and `quic.Stream`.
   - `Registry` struct with a `sync.RWMutex`-protected `map[string]*ClientConn`.
   - Methods: `Add(id string, conn *ClientConn) error`, `Remove(id string)`, `Get(id string) *ClientConn`, `Count() int`.
   - `Add` enforces the 16-client limit and rejects duplicate IDs.

### 6b: Connection Handler

1. Create `server/handler.go` with:
   - `handleConnection(ctx context.Context, conn quic.Connection, registry *Registry)`:
     - Accept stream.
     - Read first envelope; validate it is a `Register`.
     - Validate client ID (length ≤ 32, not duplicate, count < 16).
     - On success, add to registry; on failure, send `Error` and close.
     - Enter message loop: read envelopes, validate, route.
     - On stream/connection error, remove from registry.
   - `routeMessage(registry *Registry, sender string, msg *proto.Message) error`:
     - Validate content length ≤ 250,000.
     - Look up destination client; if not found, return error.
     - Write envelope to destination's stream.
     - If write fails, remove dead client and return error.

### 6c: Server Main

1. Create `server/main.go` with:
   - Parse `ip:port` from `os.Args`.
   - Generate TLS cert via `tlsutil.GenerateSelfSignedCert()`.
   - Configure QUIC listener with `MaxIdleTimeout` and TLS config.
   - Accept connections in a loop, spawn `handleConnection` in a goroutine per connection.
   - Signal handling: listen for `SIGINT`/`SIGTERM`, cancel context, close listener, close all connections via registry.

**Files created**:
- `server/main.go`
- `server/registry.go`
- `server/handler.go`

---

## Step 7: Client — Core Implementation

**Goal**: Build the client's connection, registration, read loop, and stdin write loop.

1. Create `client/main.go` with:
   - Parse `client-id` and `server-ip:port` from `os.Args`.
   - Dial QUIC connection with `InsecureSkipVerify: true`.
   - Open a bidirectional stream.
   - Send `Register` envelope.
   - Start **read loop** in a goroutine:
     - Read envelopes from the stream.
     - `Message` → print `[from_id]: content` to stdout.
     - `Error` → print to stderr and signal termination.
   - **Write loop** on main goroutine:
     - Read lines from stdin using `bufio.Scanner`.
     - Parse `<to_id>:<content>` (split on first `:`).
     - Validate content length ≤ 250,000.
     - Construct `Message` envelope and write to stream.
   - Signal handling: listen for `SIGINT`, close stream and connection, exit.

**Files created**:
- `client/main.go`

---

## Step 8: Build & Smoke Test

**Goal**: Verify the system works end-to-end manually.

1. Build both binaries:
   - `go build -o bin/server ./server/`
   - `go build -o bin/client ./client/`
2. Manual smoke test:
   - Start server: `./bin/server 127.0.0.1:4433`
   - Start client A: `./bin/client alice 127.0.0.1:4433`
   - Start client B: `./bin/client bob 127.0.0.1:4433`
   - From alice's stdin: `bob:hello bob` → verify bob sees the message.
   - From bob's stdin: `alice:hi alice` → verify alice sees the message.
   - Test error cases: send to nonexistent client, duplicate client ID, Ctrl-C shutdown.

---

## Step 9: Integration Tests

**Goal**: Automated tests covering the key scenarios.

1. Create `test/integration_test.go` with tests that programmatically start a server and connect clients (no stdin — construct and send envelopes directly):
   - **Test registration**: client registers successfully; verify server accepts.
   - **Test duplicate ID rejection**: two clients with same ID; verify second gets error.
   - **Test max clients**: register 17 clients; verify 17th gets error.
   - **Test message routing**: alice sends to bob; verify bob receives it.
   - **Test unknown destination**: send to unregistered ID; verify sender gets error.
   - **Test oversized content**: send content > 250,000 chars; verify sender gets error.
   - **Test client disconnect**: disconnect a client; verify server removes it from registry.
   - **Test server shutdown**: send SIGINT to server; verify all connections close.

**Files created**:
- `test/integration_test.go`

---

## Step 10: Cleanup & Final Verification

**Goal**: Ensure code quality and everything builds/passes cleanly.

1. Run `go vet ./...`
2. Run all tests: `go test ./...`
3. Verify clean build of both binaries.
4. Review for any hardcoded values that should be constants.

---

## File Summary

| File                          | Step | Description                              |
| ----------------------------- | ---- | ---------------------------------------- |
| `go.mod`                      | 1    | Module definition                        |
| `internal/proto/talkers.proto`| 2    | Protobuf schema                          |
| `internal/proto/talkers.pb.go`| 2    | Generated protobuf Go code               |
| `internal/errors/errors.go`   | 3    | Shared error string constants            |
| `internal/framing/framing.go` | 4    | Length-delimited envelope I/O            |
| `internal/tlsutil/cert.go`    | 5    | Self-signed TLS certificate generation   |
| `server/main.go`              | 6    | Server entry point and QUIC listener     |
| `server/registry.go`          | 6    | Client registry (map + mutex)            |
| `server/handler.go`           | 6    | Per-connection handler and message router |
| `client/main.go`              | 7    | Client entry point, read/write loops     |
| `test/framing_test.go`        | 4    | Framing unit tests                       |
| `test/tlsutil_test.go`        | 5    | TLS cert unit tests                      |
| `test/integration_test.go`    | 9    | End-to-end integration tests             |

---

## Dependency Graph

```
Step 1 (project init)
  └── Step 2 (protobuf)
       ├── Step 3 (error constants)
       ├── Step 4 (framing) ─── depends on Step 2
       └── Step 5 (TLS cert)
            ├── Step 6 (server) ─── depends on Steps 2, 3, 4, 5
            └── Step 7 (client) ─── depends on Steps 2, 3, 4
                 └── Step 8 (smoke test) ─── depends on Steps 6, 7
                      └── Step 9 (integration tests) ─── depends on Step 8
                           └── Step 10 (cleanup)
```
