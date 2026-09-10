package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	CanonicalStudentTurnIdentitySchemaV1    = "canonical-student-turn-identity.v1"
	CanonicalStudentTurnInteractionSchemaV1 = "canonical-student-turn-interaction.v1"

	CanonicalStudentTurnInteractionActionReceipt = "teacher_action_receipt"

	MaxCanonicalStudentTurnInteractionIDBytes   = 128
	CanonicalStudentTurnSourceUIDigestHexLength = 64
)

// TrustedCanonicalStudentTurnInteraction is the deliberately minimal private
// metadata accepted only by the trusted Teacher-to-Dialog materialization
// command and persisted in its private receipt ledger. It is not a browser
// input, public Message/View field, WebSocket payload, or ordinary lifecycle
// event field.
//
// Dialog never resolves the action semantics. action_receipt_id names a
// Teacher-owned, already validated receipt; source_prompt_* and the opaque
// block/action IDs are evidence Teacher validated before this trusted command.
// Teacher remains responsible for interpreting that receipt and applying any
// Student-owned state transition. Dialog persists this versioned evidence but
// does not interpret pedagogical action semantics.
type TrustedCanonicalStudentTurnInteraction struct {
	Schema                     string    `json:"schema"`
	Kind                       string    `json:"kind"`
	ActionReceiptID            uuid.UUID `json:"action_receipt_id"`
	SourcePromptMessageID      uuid.UUID `json:"source_prompt_message_id"`
	SourcePromptMessageVersion int       `json:"source_prompt_message_version"`
	BlockID                    string    `json:"block_id"`
	ActionID                   string    `json:"action_id"`
	SourceUIDigest             string    `json:"source_ui_digest"`
}

func NewTrustedCanonicalStudentTurnInteraction(input TrustedCanonicalStudentTurnInteraction) (TrustedCanonicalStudentTurnInteraction, error) {
	input.BlockID = strings.TrimSpace(input.BlockID)
	input.ActionID = strings.TrimSpace(input.ActionID)
	input.SourceUIDigest = strings.ToLower(strings.TrimSpace(input.SourceUIDigest))
	if input.Schema != CanonicalStudentTurnInteractionSchemaV1 ||
		input.Kind != CanonicalStudentTurnInteractionActionReceipt || input.ActionReceiptID == uuid.Nil ||
		input.SourcePromptMessageID == uuid.Nil || input.SourcePromptMessageVersion < 1 ||
		!validCanonicalStudentTurnInteractionID(input.BlockID) || !validCanonicalStudentTurnInteractionID(input.ActionID) ||
		!validCanonicalStudentTurnSourceUIDigest(input.SourceUIDigest) {
		return TrustedCanonicalStudentTurnInteraction{}, fmt.Errorf("%w: invalid canonical student turn interaction", ErrValidation)
	}
	return input, nil
}

func validCanonicalStudentTurnInteractionID(value string) bool {
	if value == "" || len(value) > MaxCanonicalStudentTurnInteractionIDBytes || !utf8.ValidString(value) {
		return false
	}
	if (value[0] < 'a' || value[0] > 'z') && (value[0] < 'A' || value[0] > 'Z') && (value[0] < '0' || value[0] > '9') {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '.' && character != '_' && character != '-' && character != ':' {
			return false
		}
	}
	return true
}

func validCanonicalStudentTurnSourceUIDigest(value string) bool {
	if len(value) != CanonicalStudentTurnSourceUIDigestHexLength {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// CanonicalStudentTurnIdentity binds one Teacher-owned action receipt to the
// one normal Student Dialog message that materializes it. The
// correlation ID remains the receipt identity across the future chain. Its
// causation ID is that receipt: the immediate predecessor of canonical
// materialization. The source prompt in Interaction is evidence used to create
// and validate the receipt, not the materialization's causation ID.
//
// CanonicalMessageCommandID is a deterministic trusted-command idempotency
// key, distinct from the receipt identity. Dialog creates the message through
// its exact-token internal command, sets reply_to to
// Interaction.SourcePromptMessageID, and never allows a browser to submit
// either identity or interaction metadata.
//
// It is intentionally not embedded in Message, View, browser HTTP input,
// WebSocket output, or the current V1 outbox event. Its one internal command
// input and private ledger are the only Dialog storage/transport boundaries.
type CanonicalStudentTurnIdentity struct {
	Schema                    string                                 `json:"schema"`
	DialogID                  uuid.UUID                              `json:"dialog_id"`
	StudentID                 uuid.UUID                              `json:"student_id"`
	PersonalTeacherID         uuid.UUID                              `json:"personal_teacher_id"`
	ActionReceiptID           uuid.UUID                              `json:"action_receipt_id"`
	CorrelationID             uuid.UUID                              `json:"correlation_id"`
	CausationID               uuid.UUID                              `json:"causation_id"`
	CanonicalMessageCommandID uuid.UUID                              `json:"canonical_message_command_id"`
	Interaction               TrustedCanonicalStudentTurnInteraction `json:"interaction"`
}

func NewCanonicalStudentTurnIdentity(input CanonicalStudentTurnIdentity) (CanonicalStudentTurnIdentity, error) {
	interaction, err := NewTrustedCanonicalStudentTurnInteraction(input.Interaction)
	if err != nil || input.Schema != CanonicalStudentTurnIdentitySchemaV1 ||
		input.DialogID == uuid.Nil || input.StudentID == uuid.Nil || input.PersonalTeacherID == uuid.Nil ||
		input.ActionReceiptID == uuid.Nil || input.CorrelationID != input.ActionReceiptID ||
		input.CausationID != input.ActionReceiptID ||
		input.CanonicalMessageCommandID == uuid.Nil || interaction.ActionReceiptID != input.ActionReceiptID {
		return CanonicalStudentTurnIdentity{}, fmt.Errorf("%w: invalid canonical student turn identity", ErrValidation)
	}
	input.Interaction = interaction
	return input, nil
}

// CanonicalAssistantUIDigest binds a trusted action receipt to the exact
// normalized stored assistant-ui source without retaining that UI in the
// private receipt ledger.
func CanonicalAssistantUIDigest(input json.RawMessage) (string, error) {
	normalized, err := NormalizeAssistantUI(input)
	if err != nil || len(normalized) == 0 {
		return "", fmt.Errorf("%w: canonical student turn source UI is unavailable", ErrValidation)
	}
	digest := sha256.Sum256(normalized)
	return hex.EncodeToString(digest[:]), nil
}

// CanonicalStudentTurn is the private durable receipt ledger. It records
// trusted identity/evidence only: canonical body, raw browser data,
// assistant-ui JSON, and Student-owned state are intentionally absent.
type CanonicalStudentTurn struct {
	Identity                  CanonicalStudentTurnIdentity
	CanonicalStudentMessageID uuid.UUID
	CreatedAt                 time.Time
}

func NewCanonicalStudentTurn(input CanonicalStudentTurn) (CanonicalStudentTurn, error) {
	identity, err := NewCanonicalStudentTurnIdentity(input.Identity)
	if err != nil || input.CanonicalStudentMessageID == uuid.Nil || input.CreatedAt.IsZero() {
		return CanonicalStudentTurn{}, fmt.Errorf("%w: invalid canonical student turn ledger", ErrValidation)
	}
	input.Identity = identity
	input.CreatedAt = input.CreatedAt.UTC()
	return input, nil
}

// SameCanonicalStudentTurnRequest is deliberately strict. It is used for
// exact replay only; any reused receipt/command/source action with different
// identity or canonical body is a conflict rather than a second turn.
func SameCanonicalStudentTurnIdentity(left, right CanonicalStudentTurnIdentity) bool {
	return left == right
}
