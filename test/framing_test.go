package test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/dmh2000/talkers/internal/framing"
	pb "github.com/dmh2000/talkers/internal/proto"
)

// mockStream implements io.ReadWriter for testing
type mockStream struct {
	*bytes.Buffer
}

func newMockStream() *mockStream {
	return &mockStream{Buffer: &bytes.Buffer{}}
}

// TestRoundTripRegister tests writing and reading a Register envelope
func TestRoundTripRegister(t *testing.T) {
	stream := newMockStream()

	// Create a Register envelope
	env := &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				From: "alice",
			},
		},
	}

	// Write the envelope
	if err := framing.WriteEnvelope(stream, env); err != nil {
		t.Fatalf("WriteEnvelope failed: %v", err)
	}

	// Read the envelope back
	readEnv, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("ReadEnvelope failed: %v", err)
	}

	// Verify the payload type
	register, ok := readEnv.Payload.(*pb.Envelope_Register)
	if !ok {
		t.Fatalf("Expected Register payload, got %T", readEnv.Payload)
	}

	// Verify the content
	if register.Register.From != "alice" {
		t.Errorf("Expected from='alice', got '%s'", register.Register.From)
	}
}

// TestRoundTripError tests writing and reading an Error envelope
func TestRoundTripError(t *testing.T) {
	stream := newMockStream()

	// Create an Error envelope
	env := &pb.Envelope{
		Payload: &pb.Envelope_Error{
			Error: &pb.Error{
				Error: "something went wrong",
			},
		},
	}

	// Write the envelope
	if err := framing.WriteEnvelope(stream, env); err != nil {
		t.Fatalf("WriteEnvelope failed: %v", err)
	}

	// Read the envelope back
	readEnv, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("ReadEnvelope failed: %v", err)
	}

	// Verify the payload type
	errPayload, ok := readEnv.Payload.(*pb.Envelope_Error)
	if !ok {
		t.Fatalf("Expected Error payload, got %T", readEnv.Payload)
	}

	// Verify the content
	if errPayload.Error.Error != "something went wrong" {
		t.Errorf("Expected error='something went wrong', got '%s'", errPayload.Error.Error)
	}
}

// TestRoundTripMessage tests writing and reading a Message envelope
func TestRoundTripMessage(t *testing.T) {
	stream := newMockStream()

	// Create a Message envelope
	env := &pb.Envelope{
		Payload: &pb.Envelope_Message{
			Message: &pb.Message{
				FromId:  "alice",
				ToId:    "bob",
				Content: "Hello, Bob!",
			},
		},
	}

	// Write the envelope
	if err := framing.WriteEnvelope(stream, env); err != nil {
		t.Fatalf("WriteEnvelope failed: %v", err)
	}

	// Read the envelope back
	readEnv, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("ReadEnvelope failed: %v", err)
	}

	// Verify the payload type
	message, ok := readEnv.Payload.(*pb.Envelope_Message)
	if !ok {
		t.Fatalf("Expected Message payload, got %T", readEnv.Payload)
	}

	// Verify the content
	if message.Message.FromId != "alice" {
		t.Errorf("Expected from_id='alice', got '%s'", message.Message.FromId)
	}
	if message.Message.ToId != "bob" {
		t.Errorf("Expected to_id='bob', got '%s'", message.Message.ToId)
	}
	if message.Message.Content != "Hello, Bob!" {
		t.Errorf("Expected content='Hello, Bob!', got '%s'", message.Message.Content)
	}
}

// TestRoundTripLargeMessage tests writing and reading a large message (near max size)
func TestRoundTripLargeMessage(t *testing.T) {
	stream := newMockStream()

	// Create a large content string (250,000 characters)
	largeContent := strings.Repeat("a", 250000)

	// Create a Message envelope with large content
	env := &pb.Envelope{
		Payload: &pb.Envelope_Message{
			Message: &pb.Message{
				FromId:  "alice",
				ToId:    "bob",
				Content: largeContent,
			},
		},
	}

	// Write the envelope
	if err := framing.WriteEnvelope(stream, env); err != nil {
		t.Fatalf("WriteEnvelope failed: %v", err)
	}

	// Read the envelope back
	readEnv, err := framing.ReadEnvelope(stream)
	if err != nil {
		t.Fatalf("ReadEnvelope failed: %v", err)
	}

	// Verify the payload type
	message, ok := readEnv.Payload.(*pb.Envelope_Message)
	if !ok {
		t.Fatalf("Expected Message payload, got %T", readEnv.Payload)
	}

	// Verify the content length
	if len(message.Message.Content) != 250000 {
		t.Errorf("Expected content length 250000, got %d", len(message.Message.Content))
	}

	// Verify the content matches
	if message.Message.Content != largeContent {
		t.Errorf("Content mismatch")
	}
}

// TestOversizedFrameRejection tests that frames exceeding MaxFrameSize are rejected
func TestOversizedFrameRejection(t *testing.T) {
	stream := newMockStream()

	// Manually write a frame with length exceeding MaxFrameSize
	// Write length prefix: MaxFrameSize + 1
	oversizedLength := uint32(framing.MaxFrameSize + 1)
	lengthBytes := make([]byte, 4)
	lengthBytes[0] = byte(oversizedLength >> 24)
	lengthBytes[1] = byte(oversizedLength >> 16)
	lengthBytes[2] = byte(oversizedLength >> 8)
	lengthBytes[3] = byte(oversizedLength)

	if _, err := stream.Write(lengthBytes); err != nil {
		t.Fatalf("Failed to write length prefix: %v", err)
	}

	// Attempt to read the envelope
	_, err := framing.ReadEnvelope(stream)
	if err == nil {
		t.Fatal("Expected error for oversized frame, got nil")
	}

	// Verify the error message mentions exceeding MaxFrameSize
	errMsg := err.Error()
	if !strings.Contains(errMsg, "exceeds MaxFrameSize") {
		t.Errorf("Expected error about MaxFrameSize, got: %s", errMsg)
	}
}

// TestZeroLengthFrameRejection tests that frames with zero length are rejected
func TestZeroLengthFrameRejection(t *testing.T) {
	stream := newMockStream()

	// Write a zero-length frame
	lengthBytes := make([]byte, 4) // All zeros
	if _, err := stream.Write(lengthBytes); err != nil {
		t.Fatalf("Failed to write length prefix: %v", err)
	}

	// Attempt to read the envelope
	_, err := framing.ReadEnvelope(stream)
	if err == nil {
		t.Fatal("Expected error for zero-length frame, got nil")
	}

	// Verify the error message mentions zero frame size
	errMsg := err.Error()
	if !strings.Contains(errMsg, "frame size cannot be zero") {
		t.Errorf("Expected error about zero frame size, got: %s", errMsg)
	}
}

// TestReadEnvelopeEOF tests that ReadEnvelope returns an appropriate error on EOF
func TestReadEnvelopeEOF(t *testing.T) {
	stream := newMockStream()

	// Attempt to read from an empty stream (EOF immediately)
	_, err := framing.ReadEnvelope(stream)
	if err == nil {
		t.Fatal("Expected error when reading from empty stream, got nil")
	}

	// Verify we get an EOF-related error
	if err != io.EOF && !strings.Contains(err.Error(), "EOF") {
		t.Errorf("Expected EOF error, got: %v", err)
	}
}

// TestReadEnvelopeIncompletePayload tests handling of incomplete payload reads
func TestReadEnvelopeIncompletePayload(t *testing.T) {
	stream := newMockStream()

	// Write a length prefix indicating 100 bytes
	lengthBytes := make([]byte, 4)
	lengthBytes[3] = 100 // 100 in big-endian

	if _, err := stream.Write(lengthBytes); err != nil {
		t.Fatalf("Failed to write length prefix: %v", err)
	}

	// Write only 50 bytes of payload (incomplete)
	payload := make([]byte, 50)
	if _, err := stream.Write(payload); err != nil {
		t.Fatalf("Failed to write incomplete payload: %v", err)
	}

	// Attempt to read the envelope
	_, err := framing.ReadEnvelope(stream)
	if err == nil {
		t.Fatal("Expected error for incomplete payload, got nil")
	}

	// Verify we get an EOF or read error
	if !strings.Contains(err.Error(), "EOF") && !strings.Contains(err.Error(), "payload") {
		t.Errorf("Expected EOF or payload error, got: %v", err)
	}
}
