package framing

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/proto"
	pb "github.com/dmh2000/talkers/internal/proto"
)

// MaxFrameSize is the maximum allowed size for a single frame.
// 512 KB accommodates 250K character content (up to 750KB in UTF-8) plus protobuf overhead.
const MaxFrameSize = 512 * 1024

// MaxIdleTimeout is the QUIC connection idle timeout used by both client and server.
const MaxIdleTimeout = 6000 * time.Second

// WriteEnvelope serializes the envelope using protobuf, writes a 4-byte big-endian
// length prefix, then writes the payload bytes to the stream.
func WriteEnvelope(stream io.Writer, env *pb.Envelope) error {
	// Serialize the envelope to protobuf format
	data, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// Check if the serialized data exceeds MaxFrameSize
	if len(data) > MaxFrameSize {
		return fmt.Errorf("envelope size %d exceeds MaxFrameSize %d", len(data), MaxFrameSize)
	}

	// Write the 4-byte big-endian length prefix
	lengthPrefix := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthPrefix, uint32(len(data)))
	if _, err := stream.Write(lengthPrefix); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write the payload bytes
	if _, err := stream.Write(data); err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}

	return nil
}

// ReadEnvelope reads a 4-byte length prefix, validates it against MaxFrameSize,
// allocates a buffer, reads the payload, and unmarshals the protobuf envelope.
func ReadEnvelope(stream io.Reader) (*pb.Envelope, error) {
	// Read the 4-byte length prefix
	lengthPrefix := make([]byte, 4)
	if _, err := io.ReadFull(stream, lengthPrefix); err != nil {
		return nil, fmt.Errorf("failed to read length prefix: %w", err)
	}

	// Parse the length
	length := binary.BigEndian.Uint32(lengthPrefix)

	// Validate the length against MaxFrameSize
	if length > MaxFrameSize {
		return nil, fmt.Errorf("frame size %d exceeds MaxFrameSize %d", length, MaxFrameSize)
	}

	// Validate that length is not zero
	if length == 0 {
		return nil, fmt.Errorf("frame size cannot be zero")
	}

	// Allocate buffer for the payload
	payload := make([]byte, length)

	// Read the payload
	if _, err := io.ReadFull(stream, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	// Unmarshal the protobuf envelope
	env := &pb.Envelope{}
	if err := proto.Unmarshal(payload, env); err != nil {
		return nil, fmt.Errorf("failed to unmarshal envelope: %w", err)
	}

	return env, nil
}
