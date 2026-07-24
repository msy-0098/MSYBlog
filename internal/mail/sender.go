package mail

import "context"

// Sender delivers a fully built RFC 5322 message to one recipient.
type Sender interface {
	Send(ctx context.Context, to string, message []byte) error
}
