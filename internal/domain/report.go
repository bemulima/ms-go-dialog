package domain

import (
	"errors"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrReportNotFound = errors.New("message report not found")

type ReportResolution struct {
	RequestID  uuid.UUID `json:"request_id"`
	ActorID    uuid.UUID `json:"actor_id"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	ResolvedAt time.Time `json:"resolved_at"`
}
type MessageReport struct {
	ID               uuid.UUID         `json:"id"`
	RequestID        uuid.UUID         `json:"request_id"`
	ReporterID       uuid.UUID         `json:"reporter_id"`
	MessageID        uuid.UUID         `json:"message_id"`
	DialogID         uuid.UUID         `json:"dialog_id"`
	MessageVersion   int               `json:"message_version"`
	Category         string            `json:"category"`
	Details          string            `json:"details"`
	Status           string            `json:"status"`
	Revision         int               `json:"revision"`
	CreatedAt        time.Time         `json:"created_at"`
	Resolution       *ReportResolution `json:"resolution"`
	CommandDigest    string            `json:"-"`
	ResolutionDigest string            `json:"-"`
	MentorBindingID  *uuid.UUID        `json:"-"`
}

func ValidReportText(s string, maximum int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > maximum {
		return false
	}
	for _, c := range s {
		if c == utf8.RuneError || (c < 32 && c != '\n' && c != '\t') {
			return false
		}
	}
	return true
}
func (r MessageReport) Valid() bool {
	if r.ID == uuid.Nil || r.RequestID == uuid.Nil || r.ReporterID == uuid.Nil || r.MessageID == uuid.Nil || r.DialogID == uuid.Nil || r.MessageVersion < 1 || r.CreatedAt.IsZero() || len(r.CommandDigest) != 64 || !ValidReportText(r.Details, 1000) {
		return false
	}
	if r.Category != "SPAM" && r.Category != "HARASSMENT" && r.Category != "OTHER" {
		return false
	}
	if r.MentorBindingID != nil && *r.MentorBindingID == uuid.Nil {
		return false
	}
	if r.Status == "OPEN" {
		return r.Revision == 1 && r.Resolution == nil && r.ResolutionDigest == ""
	}
	v := r.Resolution
	return r.Status == "RESOLVED" && r.Revision == 2 && len(r.ResolutionDigest) == 64 && v != nil && v.RequestID != uuid.Nil && v.ActorID != uuid.Nil && (v.Action == "HIDE" || v.Action == "DISMISS") && ValidReportText(v.Reason, 500) && !v.ResolvedAt.IsZero()
}
