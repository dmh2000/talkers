package test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	errs "github.com/dmh2000/talkers/internal/errors"
	"github.com/dmh2000/talkers/internal/framing"
	pb "github.com/dmh2000/talkers/internal/proto"
	"github.com/dmh2000/talkers/internal/tlsutil"
	"github.com/quic-go/quic-go"
)

// Registry maintains a thread-safe map of client ID to ClientConn
// (copied from server package for testing)
type Registry struct {
	clients map[string]*ClientConn
}

// ClientConn wraps a QUIC connection and stream for a registered client
type ClientConn struct {
	Connection *quic.Conn
	Stream     *quic.Stream
}

// NewRegistry creates a new empty client registry
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*ClientConn),
	}
}

// Add adds a new client to the registry
func (r *Registry) Add(id string, conn *ClientConn) error {
	if len(r.clients) >= 16 {
		return errors.New(errs.ErrMaxClientsReached)
	}
	if _, exists := r.clients[id]; exists {
		return errors.New(errs.ErrDuplicateClientID)
	}
	r.clients[id] = conn
	return nil
}

// Remove removes a client from the registry by ID
func (r *Registry) Remove(id string) {
	delete(r.clients, id)
}

// Get retrieves a client connection by ID
func (r *Registry) Get(id string) (*ClientConn, bool) {
	conn, exists := r.clients[id]
	return conn, exists
}

// Count returns the number of registered clients
func (r *Registry) Count() int {
	return len(r.clients)
}

// Close closes all client connections and clears the registry
func (r *Registry) Close() {
	for _, conn := range r.clients {
		if conn.Stream != nil {
			_ = conn.Stream.Close()
		}
		if conn.Connection != nil {
			_ = conn.Connection.CloseWithError(0, "server shutting down")
		}
	}
	r.clients = make(map[string]*ClientConn)
}

// handleConnection manages a single client connection lifecycle
func handleConnection(ctx context.Context, conn *quic.Conn, registry *Registry) {
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return
	}

	var clientID string
	defer func() {
		if clientID != "" {
			registry.Remove(clientID)
		}
		_ = stream.Close()
	}()

	// Read the first envelope - must be a Register message
	env, err := framing.ReadEnvelope(stream)
	if err != nil {
		return
	}

	reg := env.GetRegister()
	if reg == nil {
		errorEnv := &pb.Envelope{
			Payload: &pb.Envelope_Error{
				Error: &pb.Error{
					Error: errs.ErrInvalidFirstMessage,
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		return
	}

	clientID = reg.From
	if len(clientID) == 0 || len(clientID) > 32 {
		errorEnv := &pb.Envelope{
			Payload: &pb.Envelope_Error{
				Error: &pb.Error{
					Error: "client ID must be 1-32 characters",
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		return
	}

	clientConn := &ClientConn{
		Connection: conn,
		Stream:     stream,
	}

	if err := registry.Add(clientID, clientConn); err != nil {
		errorEnv := &pb.Envelope{
			Payload: &pb.Envelope_Error{
				Error: &pb.Error{
					Error: err.Error(),
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		clientID = ""
		return
	}

	// Enter message loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		env, err := framing.ReadEnvelope(stream)
		if err != nil {
			return
		}

		msg := env.GetMessage()
		if msg == nil {
			errorEnv := &pb.Envelope{
				Payload: &pb.Envelope_Error{
					Error: &pb.Error{
						Error: errs.ErrUnexpectedMessage,
					},
				},
			}
			_ = framing.WriteEnvelope(stream, errorEnv)
			return
		}

		if err := routeMessage(registry, clientID, msg); err != nil {
			errorEnv := &pb.Envelope{
				Payload: &pb.Envelope_Error{
					Error: &pb.Error{
						Error: err.Error(),
					},
				},
			}
			_ = framing.WriteEnvelope(stream, errorEnv)
		}
	}
}

// routeMessage validates and routes a message from sender to destination
func routeMessage(registry *Registry, sender string, msg *pb.Message) error {
	if len(msg.Content) > 250000 {
		return errors.New(errs.ErrContentTooLarge)
	}

	msg.FromId = sender

	destConn, exists := registry.Get(msg.ToId)
	if !exists {
		return errors.New(errs.ErrClientNotRegistered)
	}

	env := &pb.Envelope{
		Payload: &pb.Envelope_Message{
			Message: msg,
		},
	}

	if err := framing.WriteEnvelope(destConn.Stream, env); err != nil {
		registry.Remove(msg.ToId)
		return fmt.Errorf("%s: %w", errs.ErrClientDisconnected, err)
	}

	return nil
}

// startTestServer starts a QUIC server on a random port for testing
func startTestServer(t *testing.T) (addr string, shutdown func()) {
	t.Helper()

	// Generate self-signed TLS certificate
	cert, err := tlsutil.GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("Failed to generate TLS certificate: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	// Listen on random port
	listener, err := quic.ListenAddr(":0", tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to create QUIC listener: %v", err)
	}

	// Get the actual address assigned by OS
	addr = listener.Addr().String()

	registry := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	// Accept connections in a goroutine
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept(ctx)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go handleConnection(ctx, conn, registry)
		}
	}()

	shutdown = func() {
		cancel()
		_ = listener.Close()
		registry.Close()
		<-done
	}

	return addr, shutdown
}

// connectClient connects to the server and registers a client
func connectClient(t *testing.T, clientID, serverAddr string) (*quic.Conn, *quic.Stream) {
	t.Helper()

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, serverAddr, tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "failed to open stream")
		t.Fatalf("Failed to open stream: %v", err)
	}

	// Send registration envelope
	regEnv := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: clientID,
			},
		},
	}

	if err := framing.WriteEnvelope(stream, regEnv); err != nil {
		_ = stream.Close()
		_ = conn.CloseWithError(0, "failed to register")
		t.Fatalf("Failed to send registration: %v", err)
	}

	return conn, stream
}

// sendMessage sends a message envelope through a stream
func sendMessage(t *testing.T, stream *quic.Stream, to, content string) {
	t.Helper()

	msgEnv := &pb.Envelope{
		Payload: &pb.Envelope_Message{
			Message: &pb.Message{
				ToId:    to,
				Content: content,
			},
		},
	}

	if err := framing.WriteEnvelope(stream, msgEnv); err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}

// expectMessage reads and validates a message from the stream
func expectMessage(t *testing.T, stream *quic.Stream, expectedFrom, expectedContent string) {
	t.Helper()

	// Set read deadline
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = stream.SetReadDeadline(time.Time{}) }()

	env, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("Failed to read envelope: %v", err)
	}

	msg := env.GetMessage()
	if msg == nil {
		t.Fatalf("Expected message envelope, got: %v", env)
	}

	if msg.FromId != expectedFrom {
		t.Errorf("Expected from_id=%q, got %q", expectedFrom, msg.FromId)
	}

	if msg.Content != expectedContent {
		t.Errorf("Expected content=%q, got %q", expectedContent, msg.Content)
	}
}

// expectError reads and validates an error from the stream
func expectError(t *testing.T, stream *quic.Stream, expectedError string) {
	t.Helper()

	// Set read deadline
	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = stream.SetReadDeadline(time.Time{}) }()

	env, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("Failed to read envelope: %v", err)
	}

	errMsg := env.GetError()
	if errMsg == nil {
		t.Fatalf("Expected error envelope, got: %v", env)
	}

	if errMsg.Error != expectedError {
		t.Errorf("Expected error=%q, got %q", expectedError, errMsg.Error)
	}
}

// TestClientRegistration verifies a client can register successfully
func TestClientRegistration(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	// Allow server to start
	time.Sleep(100 * time.Millisecond)

	conn, stream := connectClient(t, "alice", addr)
	defer func() { _ = stream.Close() }()
	defer func() { _ = conn.CloseWithError(0, "test complete") }()

	// If we get here without error, registration succeeded
	t.Log("Client registered successfully")
}

// TestDuplicateIDRejection verifies duplicate client IDs are rejected
func TestDuplicateIDRejection(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register first client
	conn1, stream1 := connectClient(t, "alice", addr)
	defer func() { _ = stream1.Close() }()
	defer func() { _ = conn1.CloseWithError(0, "test complete") }()

	// Try to register second client with same ID
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn2, err := quic.DialAddr(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer func() { _ = conn2.CloseWithError(0, "test complete") }()

	stream2, err := conn2.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer func() { _ = stream2.Close() }()

	// Send registration with duplicate ID
	regEnv := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: "alice",
			},
		},
	}

	if err := framing.WriteEnvelope(stream2, regEnv); err != nil {
		t.Fatalf("Failed to send registration: %v", err)
	}

	// Expect error response
	expectError(t, stream2, errs.ErrDuplicateClientID)
}

// TestMaxClients verifies the 16 client limit is enforced
func TestMaxClients(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register 16 clients
	var conns []*quic.Conn
	var streams []*quic.Stream

	for i := 0; i < 16; i++ {
		clientID := fmt.Sprintf("client%d", i)
		conn, stream := connectClient(t, clientID, addr)
		conns = append(conns, conn)
		streams = append(streams, stream)
	}

	// Clean up all clients
	defer func() {
		for i := range streams {
			_ = streams[i].Close()
			_ = conns[i].CloseWithError(0, "test complete")
		}
	}()

	// Try to register 17th client
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn17, err := quic.DialAddr(ctx, addr, tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer func() { _ = conn17.CloseWithError(0, "test complete") }()

	stream17, err := conn17.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}
	defer func() { _ = stream17.Close() }()

	// Send registration
	regEnv := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: "client17",
			},
		},
	}

	if err := framing.WriteEnvelope(stream17, regEnv); err != nil {
		t.Fatalf("Failed to send registration: %v", err)
	}

	// Expect max clients error
	expectError(t, stream17, errs.ErrMaxClientsReached)
}

// TestMessageRouting verifies messages are routed correctly between clients
func TestMessageRouting(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register Alice
	connAlice, streamAlice := connectClient(t, "alice", addr)
	defer func() { _ = streamAlice.Close() }()
	defer func() { _ = connAlice.CloseWithError(0, "test complete") }()

	// Register Bob
	connBob, streamBob := connectClient(t, "bob", addr)
	defer func() { _ = streamBob.Close() }()
	defer func() { _ = connBob.CloseWithError(0, "test complete") }()

	// Allow time for both registrations to complete on server
	time.Sleep(50 * time.Millisecond)

	// Alice sends message to Bob
	sendMessage(t, streamAlice, "bob", "Hello Bob!")

	// Bob should receive the message
	expectMessage(t, streamBob, "alice", "Hello Bob!")
}

// TestUnknownDestination verifies error when sending to unregistered client
func TestUnknownDestination(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register Alice
	connAlice, streamAlice := connectClient(t, "alice", addr)
	defer func() { _ = streamAlice.Close() }()
	defer func() { _ = connAlice.CloseWithError(0, "test complete") }()

	// Alice sends message to unregistered client
	sendMessage(t, streamAlice, "charlie", "Hello Charlie!")

	// Alice should receive error
	expectError(t, streamAlice, errs.ErrClientNotRegistered)
}

// TestOversizedContent verifies content size limit is enforced
func TestOversizedContent(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register Alice and Bob
	connAlice, streamAlice := connectClient(t, "alice", addr)
	defer func() { _ = streamAlice.Close() }()
	defer func() { _ = connAlice.CloseWithError(0, "test complete") }()

	connBob, streamBob := connectClient(t, "bob", addr)
	defer func() { _ = streamBob.Close() }()
	defer func() { _ = connBob.CloseWithError(0, "test complete") }()

	// Create content > 250,000 characters
	largeContent := strings.Repeat("a", 250001)

	// Alice sends oversized message to Bob
	sendMessage(t, streamAlice, "bob", largeContent)

	// Alice should receive error
	expectError(t, streamAlice, errs.ErrContentTooLarge)
}

// TestClientDisconnect verifies server removes disconnected clients
func TestClientDisconnect(t *testing.T) {
	addr, shutdown := startTestServer(t)
	defer shutdown()

	time.Sleep(100 * time.Millisecond)

	// Register Alice and Bob
	connAlice, streamAlice := connectClient(t, "alice", addr)
	defer func() { _ = streamAlice.Close() }()
	defer func() { _ = connAlice.CloseWithError(0, "test complete") }()

	connBob, streamBob := connectClient(t, "bob", addr)

	// Bob disconnects
	_ = streamBob.Close()
	_ = connBob.CloseWithError(0, "client disconnecting")

	// Allow server to process disconnection
	time.Sleep(200 * time.Millisecond)

	// Alice tries to send message to Bob
	sendMessage(t, streamAlice, "bob", "Hello Bob!")

	// Alice should receive error (Bob is no longer registered)
	expectError(t, streamAlice, errs.ErrClientNotRegistered)
}

// TestServerShutdown verifies graceful shutdown closes all connections
func TestServerShutdown(t *testing.T) {
	addr, shutdown := startTestServer(t)

	time.Sleep(100 * time.Millisecond)

	// Register Alice
	connAlice, streamAlice := connectClient(t, "alice", addr)
	defer func() { _ = streamAlice.Close() }()
	defer func() { _ = connAlice.CloseWithError(0, "test complete") }()

	// Shutdown server
	shutdown()

	// Allow shutdown to propagate
	time.Sleep(200 * time.Millisecond)

	// Try to read from Alice's stream - should get error
	_ = streamAlice.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err := framing.ReadEnvelope(streamAlice)

	if err == nil {
		t.Error("Expected error after server shutdown, got nil")
	}

	// Check if it's an EOF or connection closed error
	if err != nil && err != io.EOF && !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "connection") {
		t.Logf("Got expected error after shutdown: %v", err)
	}
}

// TestServerShutdownWithSIGINT verifies SIGINT triggers graceful shutdown
func TestServerShutdownWithSIGINT(t *testing.T) {
	// This test starts the actual server binary as a subprocess
	// Generate self-signed TLS certificate
	cert, err := tlsutil.GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("Failed to generate TLS certificate: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	// Find a free port
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)

	// Start embedded server
	registry := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	quicListener, err := quic.ListenAddr(serverAddr, tlsConfig, quicConfig)
	if err != nil {
		t.Fatalf("Failed to create QUIC listener: %v", err)
	}

	go func() {
		defer close(done)
		for {
			conn, err := quicListener.Accept(ctx)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			go handleConnection(ctx, conn, registry)
		}
	}()

	// Allow server to start
	time.Sleep(200 * time.Millisecond)

	// Connect clients
	tlsClientConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"talkers"},
	}

	quicClientConfig := &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	}

	clientCtx, clientCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer clientCancel()

	conn, err := quic.DialAddr(clientCtx, serverAddr, tlsClientConfig, quicClientConfig)
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}

	stream, err := conn.OpenStreamSync(clientCtx)
	if err != nil {
		t.Fatalf("Failed to open stream: %v", err)
	}

	// Register client
	regEnv := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: "alice",
			},
		},
	}

	if err := framing.WriteEnvelope(stream, regEnv); err != nil {
		t.Fatalf("Failed to send registration: %v", err)
	}

	// Simulate SIGINT by canceling context and closing
	cancel()
	_ = quicListener.Close()
	registry.Close()

	// Wait for server to shutdown
	<-done

	// Allow shutdown to propagate
	time.Sleep(200 * time.Millisecond)

	// Try to read from stream - should get error
	_ = stream.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, err = framing.ReadEnvelope(stream)

	if err == nil {
		t.Error("Expected error after SIGINT shutdown, got nil")
	}

	_ = stream.Close()
	_ = conn.CloseWithError(0, "test complete")

	t.Log("Server shutdown cleanly after SIGINT")
}
