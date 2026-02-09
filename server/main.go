package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/dmh2000/talkers/internal/tlsutil"
)

func main() {
	// Validate command-line arguments
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <ip:port>\n", os.Args[0])
		os.Exit(1)
	}

	addr := os.Args[1]

	// Generate self-signed TLS certificate
	cert, err := tlsutil.GenerateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate TLS certificate: %v", err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"talkers"},
	}

	// Configure QUIC listener
	quicConfig := &quic.Config{
		MaxIdleTimeout: 60 * time.Second,
	}

	// Create QUIC listener
	listener, err := quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		log.Fatalf("Failed to create QUIC listener: %v", err)
	}

	log.Printf("Server listening on %s", addr)

	// Create registry
	registry := NewRegistry()

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel to track when accept loop exits
	done := make(chan struct{})

	// Accept connections in a goroutine
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept(ctx)
			if err != nil {
				// Check if context was cancelled (graceful shutdown)
				select {
				case <-ctx.Done():
					log.Println("Accept loop shutting down")
					return
				default:
					log.Printf("Failed to accept connection: %v", err)
					continue
				}
			}

			log.Printf("New connection from %s", conn.RemoteAddr())

			// Spawn goroutine to handle the connection
			go handleConnection(ctx, conn, registry)
		}
	}()

	// Wait for shutdown signal
	sig := <-sigChan
	log.Printf("Received signal %v, initiating graceful shutdown...", sig)

	// Cancel context to stop accepting new connections and signal handlers to exit
	cancel()

	// Close listener
	if err := listener.Close(); err != nil {
		log.Printf("Error closing listener: %v", err)
	}

	// Close all client connections
	registry.Close()

	// Wait for accept loop to exit
	<-done

	log.Println("Server shutdown complete")
}
