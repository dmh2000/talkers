# Talkers — Client/Server Messaging System Specification

## 1. Overview

### 1.1 Purpose

A message-brokered chat system where multiple CLI clients communicate through a central server. Clients send messages to any other connected client by name; the server receives each message and routes it to the designated recipient.

### 1.2 Technology Stack

| Component        | Choice                                  |
| ---------------- | --------------------------------------- |
| Language          | Go                                      |
| Transport         | QUIC via `github.com/quic-go/quic-go`   |
| Serialization     | Protocol Buffers (proto3)               |
| TLS               | Self-signed certificate generated at server startup |

### 1.3 Project Layout

```
talkers/
├── client/          # Client CLI application (main package)
├── server/          # Server CLI application (main package)
├── internal/        # Shared internal packages (protobuf, TLS, etc.)
├── test/            # Integration and end-to-end tests
└── prompts/         # Specifications and design documents
```

---

## 2. Command-Line Interface

### 2.1 Server

```
./server <ip:port>
```

| Argument   | Type   | Description                          | Example          |
| ---------- | ------ | ------------------------------------ | ---------------- |
| `ip:port`  | string | Address the server listens on        | `0.0.0.0:4433`   |

### 2.2 Client

```
./client <client-id> <server-ip:port>
```

| Argument         | Type   | Description                              | Example          |
| ---------------- | ------ | ---------------------------------------- | ---------------- |
| `client-id`      | string | Unique identifier for this client (max 32 characters) | `alice`          |
| `server-ip:port` | string | Address of the server to connect to      | `127.0.0.1:4433` |

---

## 3. Transport & Security

### 3.1 QUIC Configuration

- The server listens for QUIC connections on the specified `ip:port`.
- Each client opens a single QUIC connection to the server.
- One bidirectional QUIC stream is opened per client session. All messages (in both directions) flow over this single stream.

### 3.2 TLS / Certificate

- At startup, the **server** generates a self-signed X.509 certificate using Go's `crypto/x509` and `crypto/tls` standard library packages.
  - Common Name / SAN hostname: `sqirvy.xyz`
  - The certificate and private key are held in memory only (not written to disk).
- **Clients** connect with `tls.Config{InsecureSkipVerify: true}` to accept the self-signed certificate.

---

## 4. Protocol & Messages

### 4.1 Protobuf Definition

All communication uses a single envelope message (`Envelope`) with a `oneof` payload discriminator. The three payload types are `Register`, `Error`, and `Message`.

```protobuf
syntax = "proto3";
package talkers;
option go_package = "internal/proto";

message Register {
  string from = 1;       // client ID, max 32 characters
}

message Error {
  string error = 1;      // human-readable error description
}

message Message {
  string from_id = 1;    // sending client's ID
  string to_id   = 2;    // destination client's ID
  string content = 3;    // message body, max 250,000 characters
}

message Envelope {
  oneof payload {
    Register register = 1;
    Error    error    = 2;
    Message  message  = 3;
  }
}
```

### 4.2 Wire Framing

Because QUIC streams are byte-oriented, each protobuf `Envelope` must be length-delimited on the wire. Use a simple framing scheme:

```
[4 bytes: payload length N, big-endian uint32] [N bytes: serialized Envelope]
```

- Maximum frame size: the serialized `Envelope` must not exceed a reasonable upper bound (derived from the 250,000 character content limit plus overhead for other fields).
- Readers must validate the length prefix before allocating a buffer.

### 4.3 Message Flow Summary

| Scenario                    | Direction         | Payload Type | Description                                                   |
| --------------------------- | ----------------- | ------------ | ------------------------------------------------------------- |
| Client registration         | Client → Server   | `Register`   | Client sends its ID immediately after opening the stream      |
| Send a chat message         | Client → Server   | `Message`    | Client sets `from_id`, `to_id`, and `content`                 |
| Server forwards message     | Server → Client   | `Message`    | Server writes the message to the destination client's stream   |
| Server reports an error     | Server → Client   | `Error`      | Server sends an error description to the originating client    |

---

## 5. Server Behavior

### 5.1 Client Registry

- The server maintains a `map[string]*ClientConn` keyed by client ID.
- `ClientConn` wraps the `quic.Connection` and the single `quic.Stream` for that client.
- Maximum connected clients: **16**. Attempts to register beyond this limit are rejected with an error.

### 5.2 Connection Lifecycle

1. **Accept connection**: Server accepts an incoming QUIC connection.
2. **Accept stream**: Server accepts the single bidirectional stream from the client.
3. **Await REGISTER**: The first message on the stream must be a `Register` envelope. If it is not, the server closes the connection.
4. **Validate registration**:
   - If the client ID is already registered → send `Error` to the client and close the connection.
   - If 16 clients are already connected → send `Error` to the client and close the connection.
   - Otherwise → add the client to the registry.
5. **Message loop**: The server reads `Envelope` messages from the stream in a loop and routes them:
   - `Message` → look up `to_id` in the registry. If found, write the `Envelope` to that client's stream. If not found, send an `Error` back to the sender.
   - Any other payload type after registration is unexpected → send an `Error` to the client.
6. **Disconnection**: When the stream/connection closes or errors, remove the client from the registry.

### 5.3 Idle Timeout / Dead Client Detection

- An idle timeout value will be defined as a constant in the code (exact value to be determined during implementation).
- The QUIC connection's built-in idle timeout mechanism (`quic.Config.MaxIdleTimeout`) is used.
- When a connection times out or a write to a client's stream fails, the server removes the client from the registry.

### 5.4 Graceful Shutdown

- The server handles `SIGINT` (Ctrl-C) and `SIGTERM`.
- On signal receipt:
  1. Stop accepting new connections.
  2. Close all active client streams and connections.
  3. Exit cleanly.

---

## 6. Client Behavior

### 6.1 Connection Lifecycle

1. **Connect**: Open a QUIC connection to the server with `InsecureSkipVerify: true`.
2. **Open stream**: Open a single bidirectional QUIC stream.
3. **Register**: Send a `Register` envelope with the client's ID.
4. **Read loop** (goroutine): Continuously read `Envelope` messages from the stream:
   - `Message` → display `from_id` and `content` on stdout.
   - `Error` → print the error to stderr and terminate.
5. **Write loop** (main goroutine, initial implementation): Read lines from stdin. For each line, prompt or parse for a destination client ID, construct a `Message` envelope, and write it to the stream.

### 6.2 Input Format (Initial / Test Implementation)

For the initial test implementation, the client reads from stdin. The input format for sending a message:

```
<to_id>:<content>
```

The client parses the first `:` as the delimiter between the destination client ID and the message content. The client's own ID is automatically set as `from_id`.

> **Note**: This input mechanism is temporary for testing. The actual client functionality will be specified and implemented later.

### 6.3 No Reconnection

- If the connection is lost (server disconnect, timeout, or error), the client prints an error message to stderr and terminates.
- The client does **not** attempt automatic reconnection. The operator must restart the client manually.

### 6.4 No Discovery

- There is no mechanism to list connected clients.
- The client simply attempts to send messages. If the destination client is not registered, the server returns an error, and the client logs it and terminates.

### 6.5 Graceful Shutdown

- The client handles `SIGINT` (Ctrl-C).
- On signal receipt: close the stream and connection, then exit.

---

## 7. Error Handling

### 7.1 Error Conditions

All errors are communicated via the `Error` protobuf message. Error strings are defined as constants in a shared location in the codebase.

| Condition                                      | Triggered By       | Sent To          |
| ---------------------------------------------- | ------------------ | ---------------- |
| Content exceeds 250,000 characters             | Server (on receive)| Sending client   |
| Destination client ID not registered           | Server (on route)  | Sending client   |
| Client ID already registered (duplicate)       | Server (on register)| New client      |
| Maximum clients (16) reached                   | Server (on register)| New client      |
| Write to disconnected client fails             | Server (on forward)| Requesting client (error sent), dead client (removed from registry) |

### 7.2 Client Error Behavior

- When a client receives an `Error` envelope from the server, it prints the error message to stderr and terminates.

---

## 8. Constraints & Limits

| Parameter                  | Value              |
| -------------------------- | ------------------ |
| Max connected clients      | 16                 |
| Max client ID length       | 32 characters      |
| Max message content length | 250,000 characters |
| Streams per client         | 1 (bidirectional)  |
| TLS certificate hostname   | `sqirvy.xyz`       |

---

## 9. Out of Scope (for initial implementation)

- Persistent message storage or history
- Client-to-client encryption (beyond QUIC/TLS transport encryption)
- Authentication or authorization (any client can claim any unused ID)
- Broadcast or multicast messaging (messages are strictly unicast)
- Client discovery or presence notifications
- Automatic reconnection
- Message delivery acknowledgments to the sender
- Disk-persisted TLS certificates
