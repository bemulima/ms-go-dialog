package domain

import "errors"

var (
	ErrValidation          = errors.New("validation failed")
	ErrAuthentication      = errors.New("authentication required")
	ErrForbidden           = errors.New("dialog forbidden")
	ErrDialogNotFound      = errors.New("dialog not found")
	ErrDialogClosed        = errors.New("dialog closed")
	ErrMemberNotFound      = errors.New("member not found")
	ErrMemberLimit         = errors.New("member limit exceeded")
	ErrLastOwner           = errors.New("last active owner is required")
	ErrMessageNotFound     = errors.New("message not found")
	ErrMessageConflict     = errors.New("message version conflict")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrInvalidContent      = errors.New("invalid message content")
	ErrInvalidReadSequence = errors.New("invalid read sequence")
	ErrLinksDisabled       = errors.New("links are disabled")
	ErrImagesDisabled      = errors.New("images are disabled")
	ErrFilesDisabled       = errors.New("files are disabled")
	ErrInvalidAttachment   = errors.New("invalid attachment")
	ErrBlocked             = errors.New("user is blocked")
	ErrTicketInvalid       = errors.New("realtime ticket is invalid or expired")
)
