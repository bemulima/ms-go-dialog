package domain

import "github.com/google/uuid"

// MentorMCPMessageReceipt is immutable evidence of a single committed send.
type MentorMCPMessageReceipt struct {
	MentorID, RequestID, MessageID uuid.UUID
	Digest                         string
}
