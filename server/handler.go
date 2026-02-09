package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/quic-go/quic-go"
	errs "github.com/dmh2000/talkers/internal/errors"
	"github.com/dmh2000/talkers/internal/framing"
	"github.com/dmh2000/talkers/internal/proto"
)

// handleConnection manages a single client connection lifecycle
func handleConnection(ctx context.Context, conn *quic.Conn, registry *Registry) {
	// Accept the bidirectional stream from the client
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		log.Printf("Failed to accept stream: %v", err)
		return
	}

	// Variable to track client ID for cleanup
	var clientID string
	defer func() {
		// Ensure cleanup happens on function exit
		if clientID != "" {
			registry.Remove(clientID)
			log.Printf("Client %s disconnected and removed from registry", clientID)
		}
		_ = stream.Close()
	}()

	// Read the first envelope - must be a Register message
	env, err := framing.ReadEnvelope(stream)
	if err != nil {
		log.Printf("Failed to read first envelope: %v", err)
		return
	}

	// Validate that the first message is a Register message
	reg := env.GetRegister()
	if reg == nil {
		log.Printf("First message was not REGISTER")
		// Send error response
		errorEnv := &proto.Envelope{
			Payload: &proto.Envelope_Error{
				Error: &proto.Error{
					Error: errs.ErrInvalidFirstMessage,
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		return
	}

	// Validate client ID
	clientID = reg.From
	if len(clientID) == 0 || len(clientID) > 32 {
		log.Printf("Invalid client ID length: %d", len(clientID))
		errorEnv := &proto.Envelope{
			Payload: &proto.Envelope_Error{
				Error: &proto.Error{
					Error: "client ID must be 1-32 characters",
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		return
	}

	// Create ClientConn and add to registry
	clientConn := &ClientConn{
		Connection: conn,
		Stream:     stream,
	}

	if err := registry.Add(clientID, clientConn); err != nil {
		log.Printf("Failed to add client %s to registry: %v", clientID, err)
		// Send error response
		errorEnv := &proto.Envelope{
			Payload: &proto.Envelope_Error{
				Error: &proto.Error{
					Error: err.Error(),
				},
			},
		}
		_ = framing.WriteEnvelope(stream, errorEnv)
		// Clear clientID so defer doesn't try to remove it
		clientID = ""
		return
	}

	log.Printf("Client %s registered successfully (total clients: %d)", clientID, registry.Count())

	// Enter message loop
	for {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			log.Printf("Context cancelled for client %s", clientID)
			return
		default:
		}

		// Read next envelope
		env, err := framing.ReadEnvelope(stream)
		if err != nil {
			log.Printf("Client %s: error reading envelope: %v", clientID, err)
			return
		}

		// Validate that it's a Message (not Register or Error)
		msg := env.GetMessage()
		if msg == nil {
			log.Printf("Client %s: received non-Message envelope after registration", clientID)
			errorEnv := &proto.Envelope{
				Payload: &proto.Envelope_Error{
					Error: &proto.Error{
						Error: errs.ErrUnexpectedMessage,
					},
				},
			}
			_ = framing.WriteEnvelope(stream, errorEnv)
			return
		}

		// Route the message
		if err := routeMessage(registry, clientID, msg); err != nil {
			log.Printf("Client %s: error routing message to %s: %v", clientID, msg.ToId, err)
			// Send error back to sender
			errorEnv := &proto.Envelope{
				Payload: &proto.Envelope_Error{
					Error: &proto.Error{
						Error: err.Error(),
					},
				},
			}
			_ = framing.WriteEnvelope(stream, errorEnv)
		}
	}
}

// routeMessage validates and routes a message from sender to destination
func routeMessage(registry *Registry, sender string, msg *proto.Message) error {
	// Validate content length
	if len(msg.Content) > 250000 {
		return errors.New(errs.ErrContentTooLarge)
	}

	// Ensure the from_id matches the sender
	msg.FromId = sender

	// Look up destination client
	destConn, exists := registry.Get(msg.ToId)
	if !exists {
		return errors.New(errs.ErrClientNotRegistered)
	}

	// Create envelope with the message
	env := &proto.Envelope{
		Payload: &proto.Envelope_Message{
			Message: msg,
		},
	}

	// Write to destination stream
	if err := framing.WriteEnvelope(destConn.Stream, env); err != nil {
		// If write fails, remove the dead client from registry
		registry.Remove(msg.ToId)
		return fmt.Errorf("%s: %w", errs.ErrClientDisconnected, err)
	}

	log.Printf("Message routed from %s to %s (%d characters)", sender, msg.ToId, len(msg.Content))
	return nil
}
