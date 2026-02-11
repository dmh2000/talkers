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
	maxInputLength    = 256

	colorBlue  = "\033[94m"
	colorGreen = "\033[92m"
	colorCyan  = "\033[96m"
	colorReset = "\033[0m"
)

func help(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n\n", msg)
	fmt.Fprintf(os.Stderr, "Usage: %s <client-id> <server-ip:port> <model> <system-file>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  client-id       Unique identifier for this client (1-%d chars)\n", maxClientIDLength)
	fmt.Fprintf(os.Stderr, "  server-ip:port  Address of the talkers server\n")
	fmt.Fprintf(os.Stderr, "  model           LLM model name (e.g. claude-sonnet-4)\n")
	fmt.Fprintf(os.Stderr, "  system-file     Path to file containing the AI system prompt\n")
	os.Exit(1)
}

func main() {
	// Parse command-line arguments
	if len(os.Args) != 5 {
		help("expected 4 arguments")
	}

	clientID := os.Args[1]
	serverAddr := os.Args[2]
	model := os.Args[3]

	if len(clientID) > maxClientIDLength {
		help(fmt.Sprintf("client ID exceeds maximum length of %d characters", maxClientIDLength))
	}
	if len(clientID) == 0 {
		help("client ID cannot be empty")
	}
	if len(serverAddr) == 0 {
		help("server address cannot be empty")
	}
	if len(model) == 0 {
		help("model cannot be empty")
	}

	systemBytes, err := os.ReadFile(os.Args[4])
	if err != nil {
		help(fmt.Sprintf("failed to read system file: %v", err))
	}
	system := string(systemBytes)

	queryContext := []string{} // context for AI queries
	var contextMu sync.Mutex

	client, err := ai.AIClient(model)
	if err != nil {
		help(fmt.Sprintf("failed to create AI client: %v", err))
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

	// Channel for write loop input (from terminal and AI responses)
	writeChan := make(chan string, 16)

	// Start read loop in a goroutine
	go readLoop(stream, readDone, &queryContext, &contextMu, client, model, system, writeChan)

	// Channel to coordinate shutdown on stdin close
	shutdownChan := make(chan struct{})

	// Terminal input goroutine
	go terminalInput(writeChan, shutdownChan, ctx)

	// Write loop goroutine
	go writeLoop(writeChan, stream, clientID, &queryContext, &contextMu, ctx)

	// Wait for shutdown signal, read error, or stdin close
	select {
	case <-sigChan:
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, shutting down...\n")
	case err := <-readDone:
		if err != nil {
			fmt.Fprintf(os.Stderr, "Read loop terminated: %v\n", err)
		}
	case <-shutdownChan:
		// Terminal input closed
	}

	// Reset terminal color and clean shutdown
	fmt.Print(colorReset)
	cancel()
	_ = stream.Close()
	_ = conn.CloseWithError(0, "client shutting down")
}

// terminalInput reads lines from stdin, validates format and length, and sends them to writeChan.
func terminalInput(writeChan chan<- string, done chan struct{}, ctx context.Context) {
	defer close(done)
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

		// Validate input length
		if len(parts[1]) > maxInputLength {
			fmt.Fprintf(os.Stderr, "Error: input exceeds maximum length of %d characters\n", maxInputLength)
			continue
		}

		select {
		case writeChan <- line:
		case <-ctx.Done():
			return
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: reading from stdin: %v\n", err)
	}
}

// writeLoop reads messages from writeChan, sends them as envelopes, and updates the AI query context.
func writeLoop(writeChan <-chan string, stream *quic.Stream, clientID string, queryContext *[]string, contextMu *sync.Mutex, ctx context.Context) {
	for {
		select {
		case line := <-writeChan:
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}

			toID := parts[0]
			msgContent := parts[1]

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
			contextMu.Lock()
			*queryContext = ai.AIAddContext(*queryContext, clientID, msgContent)
			contextMu.Unlock()

		case <-ctx.Done():
			return
		}
	}
}

// readLoop continuously reads envelopes from the stream and processes them
func readLoop(stream *quic.Stream, done chan<- error, queryContext *[]string, contextMu *sync.Mutex, aiClient ai.Client, model string, system string, writeChan chan<- string) {
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

			// Add received message to AI query context and take a snapshot
			contextMu.Lock()
			*queryContext = ai.AIAddContext(*queryContext, msg.GetFromId(), msg.GetContent())
			contextCopy := make([]string, len(*queryContext))
			copy(contextCopy, *queryContext)
			contextMu.Unlock()

			// Query AI and send response to write loop
			response, err := ai.AIQuery(aiClient, system, contextCopy, model)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: AI query failed: %v\n", err)
			} else if len(response) > 0 {
				fmt.Printf("%s[AI]: %s%s\n", colorCyan, response, colorGreen)
				writeChan <- fmt.Sprintf("%s:%s", msg.GetFromId(), response)
			}

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
