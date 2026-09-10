package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewCanonicalStudentTurnIdentityAcceptsTrustedActionReceiptChain(t *testing.T) {
	receiptID := uuid.New()
	sourceMessageID := uuid.New()
	input := CanonicalStudentTurnIdentity{
		Schema:   CanonicalStudentTurnIdentitySchemaV1,
		DialogID: uuid.New(), StudentID: uuid.New(), PersonalTeacherID: uuid.New(),
		ActionReceiptID: receiptID, CorrelationID: receiptID,
		CausationID:               receiptID,
		CanonicalMessageCommandID: uuid.New(),
		Interaction: TrustedCanonicalStudentTurnInteraction{
			Schema: CanonicalStudentTurnInteractionSchemaV1, Kind: CanonicalStudentTurnInteractionActionReceipt,
			ActionReceiptID: receiptID, SourcePromptMessageID: sourceMessageID, SourcePromptMessageVersion: 3,
			BlockID: "onboarding.language.d0.v1", ActionID: "language.ru", SourceUIDigest: strings.Repeat("a", CanonicalStudentTurnSourceUIDigestHexLength),
		},
	}

	got, err := NewCanonicalStudentTurnIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.CorrelationID != receiptID || got.CausationID != receiptID || got.Interaction.SourcePromptMessageID != sourceMessageID ||
		got.Interaction.ActionReceiptID != receiptID {
		t.Fatalf("unexpected canonical identity: %+v", got)
	}
}

func TestNewCanonicalStudentTurnIdentityRejectsBrokenPrivateChain(t *testing.T) {
	receiptID := uuid.New()
	sourceMessageID := uuid.New()
	valid := CanonicalStudentTurnIdentity{
		Schema:   CanonicalStudentTurnIdentitySchemaV1,
		DialogID: uuid.New(), StudentID: uuid.New(), PersonalTeacherID: uuid.New(),
		ActionReceiptID: receiptID, CorrelationID: receiptID,
		CausationID:               receiptID,
		CanonicalMessageCommandID: uuid.New(),
		Interaction: TrustedCanonicalStudentTurnInteraction{
			Schema: CanonicalStudentTurnInteractionSchemaV1, Kind: CanonicalStudentTurnInteractionActionReceipt,
			ActionReceiptID: receiptID, SourcePromptMessageID: sourceMessageID, SourcePromptMessageVersion: 1,
			BlockID: "onboarding.language.d0.v1", ActionID: "language.ru", SourceUIDigest: strings.Repeat("a", CanonicalStudentTurnSourceUIDigestHexLength),
		},
	}

	tests := map[string]func(*CanonicalStudentTurnIdentity){
		"wrong schema":                 func(value *CanonicalStudentTurnIdentity) { value.Schema = "canonical-student-turn-identity.v0" },
		"correlation is not receipt":   func(value *CanonicalStudentTurnIdentity) { value.CorrelationID = uuid.New() },
		"causation is not receipt":     func(value *CanonicalStudentTurnIdentity) { value.CausationID = uuid.New() },
		"missing source prompt":        func(value *CanonicalStudentTurnIdentity) { value.Interaction.SourcePromptMessageID = uuid.Nil },
		"missing source version":       func(value *CanonicalStudentTurnIdentity) { value.Interaction.SourcePromptMessageVersion = 0 },
		"interaction receipt differs":  func(value *CanonicalStudentTurnIdentity) { value.Interaction.ActionReceiptID = uuid.New() },
		"untrusted interaction kind":   func(value *CanonicalStudentTurnIdentity) { value.Interaction.Kind = "browser_action" },
		"blank block id":               func(value *CanonicalStudentTurnIdentity) { value.Interaction.BlockID = "   " },
		"non alphanumeric block start": func(value *CanonicalStudentTurnIdentity) { value.Interaction.BlockID = ".opaque" },
		"oversized action id": func(value *CanonicalStudentTurnIdentity) {
			value.Interaction.ActionID = strings.Repeat("a", MaxCanonicalStudentTurnInteractionIDBytes+1)
		},
		"invalid source ui digest": func(value *CanonicalStudentTurnIdentity) { value.Interaction.SourceUIDigest = "not-a-digest" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := NewCanonicalStudentTurnIdentity(candidate); !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
}
