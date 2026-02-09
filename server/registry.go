package main

import (
	"errors"
	"sync"

	"github.com/quic-go/quic-go"
	errs "github.com/dmh2000/talkers/internal/errors"
)

// ClientConn wraps a QUIC connection and stream for a registered client
type ClientConn struct {
	Connection *quic.Conn
	Stream     *quic.Stream
}

// Registry maintains a thread-safe map of client ID to ClientConn
type Registry struct {
	mu      sync.RWMutex
	clients map[string]*ClientConn
}

// NewRegistry creates a new empty client registry
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*ClientConn),
	}
}

// Add adds a new client to the registry.
// Returns an error if the registry is full (16 clients) or if the client ID already exists.
func (r *Registry) Add(id string, conn *ClientConn) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if registry is at capacity
	if len(r.clients) >= 16 {
		return errors.New(errs.ErrMaxClientsReached)
	}

	// Check for duplicate client ID
	if _, exists := r.clients[id]; exists {
		return errors.New(errs.ErrDuplicateClientID)
	}

	// Add the client to the registry
	r.clients[id] = conn
	return nil
}

// Remove removes a client from the registry by ID
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
}

// Get retrieves a client connection by ID.
// Returns the ClientConn and true if found, nil and false otherwise.
func (r *Registry) Get(id string) (*ClientConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	conn, exists := r.clients[id]
	return conn, exists
}

// Count returns the number of registered clients
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// Close closes all client connections and clears the registry
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close all client connections
	for _, conn := range r.clients {
		if conn.Stream != nil {
			conn.Stream.Close()
		}
		if conn.Connection != nil {
			conn.Connection.CloseWithError(0, "server shutting down")
		}
	}

	// Clear the registry
	r.clients = make(map[string]*ClientConn)
}
