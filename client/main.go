package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/dmh2000/talkers/internal/ai"
	"github.com/dmh2000/talkers/internal/framing"
	pb "github.com/dmh2000/talkers/internal/proto"
	"github.com/quic-go/quic-go"
)

const (
	maxClientIDLength = 32
	maxContentLength  = 250000

	defaultModel = "dummy-model"

	colorBlue  = "\033[94m"
	colorGreen = "\033[92m"
	colorReset = "\033[0m"
)

func main() {
	// Parse command-line arguments
	if len(os.Args) < 3 || len(os.Args) > 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <client-id> <server-ip:port> [model]\n", os.Args[0])
		os.Exit(1)
	}

	clientID := os.Args[1]
	serverAddr := os.Args[2]

	model := defaultModel
	if len(os.Args) == 4 {
		model = os.Args[3]
	}

	content := []string{} // context for AI queries
	var contentMu sync.Mutex

	client, err := ai.AIClient(model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create AI client: %v\n", err)
		os.Exit(1)
	}
	_ = client // will be used for AI queries

	// Validate client ID length
	if len(clientID) > maxClientIDLength {
		fmt.Fprintf(os.Stderr, "Error: client ID exceeds maximum length of %d characters\n", maxClientIDLength)
		os.Exit(1)
	}

	// Set up context with cancellation for clean shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for SIGINT (Ctrl-C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)

	// Dial QUIC connection to server
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Accept self-signed certificates
		NextProtos:         []string{"talkers"},
	}

	quicConfig := &quic.Config{
		MaxIdleTimeout: framing.MaxIdleTimeout,
	}

	conn, err := quic.DialAddr(ctx, serverAddr, tlsConfig, quicConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to server: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = conn.CloseWithError(0, "client shutting down") }()

	// Open a bidirectional stream
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open stream: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = stream.Close() }()

	// Send registration message
	registerEnv := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: clientID,
			},
		},
	}

	if err := framing.WriteEnvelope(stream, registerEnv); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to send registration: %v\n", err)
		os.Exit(1)
	}

	// Channel to signal termination from read loop
	readDone := make(chan error, 1)

	// Start read loop in a goroutine
	go readLoop(stream, readDone, &content, &contentMu)

	// Channel to coordinate shutdown
	shutdownChan := make(chan struct{})

	// Write loop in main goroutine
	go func() {
		defer close(shutdownChan)
		scanner := bufio.NewScanner(os.Stdin)

		// Set input color to light green
		fmt.Print(colorGreen)

		for scanner.Scan() {
			line := scanner.Text()

			// Parse input format: <to_id>:<content>
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				fmt.Fprintf(os.Stderr, "Error: invalid input format, expected <to_id>:<content>\n")
				continue
			}

			toID := parts[0]
			msgContent := parts[1]

			// Validate content length
			if len(msgContent) > maxContentLength {
				fmt.Fprintf(os.Stderr, "Error: message content exceeds maximum length of %d characters\n", maxContentLength)
				continue
			}

			// Construct and send message envelope
			msgEnv := &pb.Envelope{
				Payload: &pb.Envelope_Message{
					Message: &pb.Message{
						FromId:  clientID,
						ToId:    toID,
						Content: msgContent,
					},
				},
			}

			if err := framing.WriteEnvelope(stream, msgEnv); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to send message: %v\n", err)
				return
			}

			// Add sent message to AI query context
			contentMu.Lock()
			content = ai.AIAddContent(content, clientID, msgContent)
			contentMu.Unlock()
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: reading from stdin: %v\n", err)
		}
	}()

	// Wait for shutdown signal, read error, or write completion
	select {
	case <-sigChan:
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, shutting down...\n")
	case err := <-readDone:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Read loop terminated: %v\n", err)
		}
	case <-shutdownChan:
		// Write loop completed (stdin closed)
	}

	// Reset terminal color and clean shutdown
	fmt.Print(colorReset)
	cancel()
	_ = stream.Close()
	_ = conn.CloseWithError(0, "client shutting down")
}

// readLoop continuously reads envelopes from the stream and processes them
func readLoop(stream *quic.Stream, done chan<- error, content *[]string, contentMu *sync.Mutex) {
	defer close(done)

	for {
		env, err := framing.ReadEnvelope(stream)
		if err != nil {
			// Handle EOF and connection closed gracefully
			if err == io.EOF {
				done <- nil
				return
			}
			if strings.Contains(err.Error(), "Application error") ||
				strings.Contains(err.Error(), "connection closed") {
				done <- nil
				return
			}
			done <- fmt.Errorf("failed to read envelope: %w", err)
			return
		}

		// Handle different envelope types
		switch payload := env.Payload.(type) {
		case *pb.Envelope_Message:
			msg := payload.Message
			fmt.Printf("%s[%s]: %s%s\n", colorBlue, msg.GetFromId(), msg.GetContent(), colorGreen)

			// Add received message to AI query context
			contentMu.Lock()
			*content = ai.AIAddContent(*content, msg.GetFromId(), msg.GetContent())
			contentMu.Unlock()

		case *pb.Envelope_Error:
			errMsg := payload.Error
			fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg.GetError())
			done <- fmt.Errorf("server error: %s", errMsg.GetError())
			return

		default:
			// Unexpected envelope type - log but continue
			fmt.Fprintf(os.Stderr, "Warning: received unexpected envelope type\n")
		}
	}
}
