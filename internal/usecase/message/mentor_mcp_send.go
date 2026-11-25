package message

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

type MentorMCPSendInput struct {
	MentorID         uuid.UUID                `json:"mentor_id"`
	RequestID        uuid.UUID                `json:"request_id"`
	Kind             domain.MentorContextKind `json:"kind"`
	SourceID         uuid.UUID                `json:"source_id"`
	DialogID         uuid.UUID                `json:"dialog_id"`
	BindingID        uuid.UUID                `json:"binding_id"`
	ExpectedRevision int64                    `json:"expected_revision"`
	Body             string                   `json:"body"`
}

type MentorMCPSendCommand struct {
	MentorMCPSendInput
	ConfirmationID uuid.UUID `json:"confirmation_id"`
}

type MentorMCPSendResult struct {
	Schema    string    `json:"schema"`
	MentorID  uuid.UUID `json:"mentor_id"`
	RequestID uuid.UUID `json:"request_id"`
	DialogID  uuid.UUID `json:"dialog_id"`
	BindingID uuid.UUID `json:"binding_id"`
	MessageID uuid.UUID `json:"message_id"`
}

func (in MentorMCPSendInput) valid() bool {
	if in.MentorID == uuid.Nil || in.RequestID == uuid.Nil || !in.Kind.Valid() || in.SourceID == uuid.Nil ||
		in.DialogID == uuid.Nil || in.BindingID == uuid.Nil || in.ExpectedRevision < 1 || in.ExpectedRevision >= 9007199254740991 ||
		len(in.Body) > 2000 || !utf8.ValidString(in.Body) || strings.TrimSpace(in.Body) == "" {
		return false
	}
	for _, ch := range in.Body {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			return false
		}
	}
	return true
}

func (in MentorMCPSendInput) checkAccess(access *domain.MentorAccess) error {
	if access == nil || access.ActorID != in.MentorID || access.Binding.MentorID != in.MentorID ||
		access.Binding.ID != in.BindingID || access.Binding.Kind != in.Kind || access.Binding.SourceID != in.SourceID ||
		access.Binding.DialogID != in.DialogID {
		return domain.ErrDialogNotFound
	}
	if access.Revision != in.ExpectedRevision {
		return domain.ErrMessageConflict
	}
	return nil
}

func (s Service) PreviewMentorMCPSend(ctx context.Context, in MentorMCPSendInput) error {
	if !in.valid() {
		return domain.ErrValidation
	}
	if s.Education == nil || s.MentorMCPReceipts == nil {
		return domain.ErrDependencyUnavailable
	}
	ctx, access, err := s.mentorStart(domain.WithMentorBinding(ctx, in.BindingID), in.MentorID, in.DialogID)
	if err != nil {
		return err
	}
	if err := in.checkAccess(access); err != nil {
		return err
	}
	_, space, _, err := s.loadWritableContext(ctx, domain.Actor{UserID: in.MentorID, Role: "MENTOR"}, in.DialogID, false)
	if err != nil {
		return err
	}
	return domain.AnalyzeMessageContent(in.Body, 0, 0).Validate(space.Policy)
}

// SendMentorMCP commits the authorized text and its immutable receipt together.
// Replays can return the original message reference without reading live text.
func (s Service) SendMentorMCP(ctx context.Context, command MentorMCPSendCommand) (MentorMCPSendResult, error) {
	in := command.MentorMCPSendInput
	if !in.valid() || command.ConfirmationID == uuid.Nil {
		return MentorMCPSendResult{}, domain.ErrValidation
	}
	if s.Education == nil || s.MentorMCPReceipts == nil || s.Tx == nil || s.Messages == nil {
		return MentorMCPSendResult{}, domain.ErrDependencyUnavailable
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return MentorMCPSendResult{}, domain.ErrValidation
	}
	digestBytes := sha256.Sum256(raw)
	digest := hex.EncodeToString(digestBytes[:])
	idempotencyKey := uuid.NewSHA1(uuid.NameSpaceOID, []byte("dialog.mentor-mcp.message.v1:"+in.MentorID.String()+":"+in.RequestID.String()))
	var accepted uuid.UUID
	err = s.Tx.WithinTransaction(ctx, func(tx context.Context) error {
		if err := s.Messages.LockIdempotencyKey(tx, in.MentorID, idempotencyKey); err != nil {
			return err
		}
		receipt, err := s.MentorMCPReceipts.GetMentorMCPMessageReceipt(tx, in.MentorID, in.RequestID)
		if err == nil {
			if receipt.MentorID != in.MentorID || receipt.RequestID != in.RequestID || receipt.MessageID == uuid.Nil {
				return domain.ErrDependencyUnavailable
			}
			if receipt.Digest != digest {
				return domain.ErrIdempotencyConflict
			}
			accepted = receipt.MessageID
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		tx = domain.WithMentorBinding(tx, in.BindingID)
		actor := domain.Actor{UserID: in.MentorID, Role: "MENTOR"}
		result, err := s.create(tx, actor, CreateInput{DialogID: in.DialogID, Body: in.Body, IdempotencyKey: idempotencyKey}, in.checkAccess)
		if err != nil {
			return err
		}
		if !result.Created {
			return domain.ErrIdempotencyConflict
		}
		item := result.View.Message
		if item.ID == uuid.Nil || item.SenderID != in.MentorID || item.DialogID != in.DialogID ||
			item.MentorBindingID == nil || *item.MentorBindingID != in.BindingID || item.MentorContextRevision != in.ExpectedRevision {
			return domain.ErrDependencyUnavailable
		}
		accepted = item.ID
		return s.MentorMCPReceipts.PutMentorMCPMessageReceipt(tx, domain.MentorMCPMessageReceipt{
			MentorID: in.MentorID, RequestID: in.RequestID, MessageID: accepted, Digest: digest,
		})
	})
	if err != nil || accepted == uuid.Nil {
		if err == nil {
			err = domain.ErrDependencyUnavailable
		}
		return MentorMCPSendResult{}, err
	}
	return MentorMCPSendResult{"dialog.mentor-mcp-message-sent.v1", in.MentorID, in.RequestID, in.DialogID, in.BindingID, accepted}, nil
}
