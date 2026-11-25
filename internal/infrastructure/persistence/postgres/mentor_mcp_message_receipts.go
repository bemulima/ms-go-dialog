package postgres

import (
	"context"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

func (r MentorRepository) GetMentorMCPMessageReceipt(ctx context.Context, mentor, command uuid.UUID) (domain.MentorMCPMessageReceipt, error) {
	var out domain.MentorMCPMessageReceipt
	err := runner(ctx, r.Pool).QueryRow(ctx, `
SELECT mentor_id, request_id, message_id, command_digest FROM dialog_mentor_mcp_message_receipt
WHERE mentor_id=$1 AND request_id=$2`, mentor, command).Scan(&out.MentorID, &out.RequestID, &out.MessageID, &out.Digest)
	return out, mapError(err)
}

func (r MentorRepository) PutMentorMCPMessageReceipt(ctx context.Context, out domain.MentorMCPMessageReceipt) error {
	_, err := runner(ctx, r.Pool).Exec(ctx, `
INSERT INTO dialog_mentor_mcp_message_receipt(mentor_id,request_id,message_id,command_digest) VALUES($1,$2,$3,$4)`,
		out.MentorID, out.RequestID, out.MessageID, out.Digest)
	return mapError(err)
}

var _ repository.MentorMCPMessageReceipts = MentorRepository{}
