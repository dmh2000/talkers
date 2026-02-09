package errors

// Error constants shared between client and server
const (
	ErrContentTooLarge       = "content exceeds 250000 character limit"
	ErrClientNotRegistered   = "destination client is not registered"
	ErrDuplicateClientID     = "client ID is already registered"
	ErrMaxClientsReached     = "maximum number of clients (16) reached"
	ErrClientDisconnected    = "destination client is disconnected"
	ErrUnexpectedMessage     = "unexpected message type after registration"
	ErrInvalidFirstMessage   = "first message must be REGISTER"
)
