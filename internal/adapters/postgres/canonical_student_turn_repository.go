package postgres

import (
	"context"
	"sort"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CanonicalStudentTurnRepository persists the private trusted-receipt ledger.
// It deliberately has no projection methods: callers can recover an exact
// command replay or enforce immutability, but never expose its metadata.
type CanonicalStudentTurnRepository struct{ Pool *pgxpool.Pool }

const canonicalStudentTurnColumns = `action_receipt_id, canonical_message_command_id,
dialog_id, student_id, personal_teacher_id, source_prompt_message_id,
source_prompt_message_version, block_id, action_id, source_ui_digest,
canonical_student_message_id, correlation_id, causation_id, created_at`

func (r CanonicalStudentTurnRepository) LockIdentity(ctx context.Context, receiptID, commandID uuid.UUID) error {
	values := []string{"canonical_student_turn:command:" + commandID.String(), "canonical_student_turn:receipt:" + receiptID.String()}
	sort.Strings(values)
	for _, value := range values {
		if _, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, value); err != nil {
			return err
		}
	}
	return nil
}

func (r CanonicalStudentTurnRepository) LockSourceAction(ctx context.Context, dialogID, promptID uuid.UUID, blockID, actionID string) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"canonical_student_turn:source:"+dialogID.String()+":"+promptID.String()+":"+blockID+":"+actionID)
	return err
}

func (r CanonicalStudentTurnRepository) GetByActionReceiptID(ctx context.Context, receiptID uuid.UUID) (domain.CanonicalStudentTurn, error) {
	return scanCanonicalStudentTurn(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+canonicalStudentTurnColumns+` FROM dialog_canonical_student_turn WHERE action_receipt_id=$1`, receiptID))
}

func (r CanonicalStudentTurnRepository) GetByCanonicalMessageCommandID(ctx context.Context, commandID uuid.UUID) (domain.CanonicalStudentTurn, error) {
	return scanCanonicalStudentTurn(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+canonicalStudentTurnColumns+` FROM dialog_canonical_student_turn WHERE canonical_message_command_id=$1`, commandID))
}

func (r CanonicalStudentTurnRepository) GetBySourceAction(ctx context.Context, dialogID, promptID uuid.UUID, blockID, actionID string) (domain.CanonicalStudentTurn, error) {
	return scanCanonicalStudentTurn(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+canonicalStudentTurnColumns+` FROM dialog_canonical_student_turn
WHERE dialog_id=$1 AND source_prompt_message_id=$2 AND block_id=$3 AND action_id=$4`, dialogID, promptID, blockID, actionID))
}

func (r CanonicalStudentTurnRepository) GetByCanonicalMessageID(ctx context.Context, messageID uuid.UUID) (domain.CanonicalStudentTurn, error) {
	return scanCanonicalStudentTurn(runner(ctx, r.Pool).QueryRow(ctx, `SELECT `+canonicalStudentTurnColumns+` FROM dialog_canonical_student_turn WHERE canonical_student_message_id=$1`, messageID))
}

func (r CanonicalStudentTurnRepository) Create(ctx context.Context, item domain.CanonicalStudentTurn) error {
	item, err := domain.NewCanonicalStudentTurn(item)
	if err != nil {
		return err
	}
	identity := item.Identity
	_, err = runner(ctx, r.Pool).Exec(ctx, `INSERT INTO dialog_canonical_student_turn (
action_receipt_id, canonical_message_command_id, dialog_id, student_id, personal_teacher_id,
source_prompt_message_id, source_prompt_message_version, block_id, action_id, source_ui_digest,
canonical_student_message_id, correlation_id, causation_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		identity.ActionReceiptID, identity.CanonicalMessageCommandID, identity.DialogID, identity.StudentID, identity.PersonalTeacherID,
		identity.Interaction.SourcePromptMessageID, identity.Interaction.SourcePromptMessageVersion, identity.Interaction.BlockID,
		identity.Interaction.ActionID, identity.Interaction.SourceUIDigest, item.CanonicalStudentMessageID,
		identity.CorrelationID, identity.CausationID, item.CreatedAt)
	return mapError(err)
}

func scanCanonicalStudentTurn(row interface{ Scan(...any) error }) (domain.CanonicalStudentTurn, error) {
	var item domain.CanonicalStudentTurn
	var identity domain.CanonicalStudentTurnIdentity
	identity.Schema = domain.CanonicalStudentTurnIdentitySchemaV1
	identity.Interaction.Schema = domain.CanonicalStudentTurnInteractionSchemaV1
	identity.Interaction.Kind = domain.CanonicalStudentTurnInteractionActionReceipt
	err := row.Scan(
		&identity.ActionReceiptID, &identity.CanonicalMessageCommandID,
		&identity.DialogID, &identity.StudentID, &identity.PersonalTeacherID, &identity.Interaction.SourcePromptMessageID,
		&identity.Interaction.SourcePromptMessageVersion, &identity.Interaction.BlockID, &identity.Interaction.ActionID,
		&identity.Interaction.SourceUIDigest, &item.CanonicalStudentMessageID, &identity.CorrelationID, &identity.CausationID, &item.CreatedAt,
	)
	if err != nil {
		return domain.CanonicalStudentTurn{}, mapError(err)
	}
	identity.Interaction.ActionReceiptID = identity.ActionReceiptID
	item.Identity = identity
	return domain.NewCanonicalStudentTurn(item)
}

var _ repository.CanonicalStudentTurnRepository = (*CanonicalStudentTurnRepository)(nil)
