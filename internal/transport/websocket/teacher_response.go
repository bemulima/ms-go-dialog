package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
)

const (
	teacherPreviewLifetime = 45 * time.Second
	teacherPreviewCapacity = 64
	teacherPreviewMaxRunes = 12000
)

type teacherResponseFrame struct {
	Version         int       `json:"v"`
	Type            string    `json:"type"`
	DialogID        uuid.UUID `json:"dialog_id"`
	MessageID       uuid.UUID `json:"message_id"`
	MessageSequence int64     `json:"message_sequence"`
	ResponseID      uuid.UUID `json:"response_id"`
	AttemptID       uuid.UUID `json:"attempt_id"`
	Attempt         int       `json:"attempt"`
	Sequence        int       `json:"sequence"`
	CorrelationID   uuid.UUID `json:"correlation_id"`
	OccurredAt      time.Time `json:"occurred_at"`
	Text            string    `json:"text,omitempty"`
}

type teacherResponsePreview struct {
	teacherResponseFrame
	StudentID uuid.UUID `json:"student_id"`
	terminal  bool
	policy    domain.Policy
}

func (preview teacherResponsePreview) follows(prior teacherResponsePreview) bool {
	switch {
	case prior.ResponseID == uuid.Nil:
		return true
	case prior.StudentID != preview.StudentID:
		return false
	case prior.MessageSequence != preview.MessageSequence:
		return prior.MessageSequence < preview.MessageSequence
	case prior.MessageID != preview.MessageID || prior.ResponseID != preview.ResponseID || prior.CorrelationID != preview.CorrelationID:
		return false
	case prior.Attempt != preview.Attempt:
		return prior.Attempt < preview.Attempt && prior.AttemptID != preview.AttemptID
	default:
		return !prior.terminal && prior.AttemptID == preview.AttemptID && prior.Sequence < preview.Sequence
	}
}

func decodeTeacherResponsePreview(payload []byte, now time.Time) (teacherResponsePreview, bool) {
	var preview teacherResponsePreview
	if len(payload) > (6*teacherPreviewMaxRunes+1024) || !utf8.Valid(payload) {
		return preview, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&preview) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		preview.Version != protocolVersion || preview.DialogID == uuid.Nil || preview.StudentID == uuid.Nil ||
		preview.MessageID == uuid.Nil || preview.ResponseID == uuid.Nil || preview.AttemptID == uuid.Nil || preview.CorrelationID == uuid.Nil ||
		preview.Attempt <= 0 || preview.Sequence <= 0 || preview.MessageSequence <= 0 ||
		preview.OccurredAt.Before(now.Add(-teacherPreviewLifetime)) || preview.OccurredAt.After(now.Add(5*time.Second)) {
		return preview, false
	}
	switch preview.Type {
	case "teacher.response.started":
		return preview, preview.Sequence == 1 && preview.Text == ""
	case "teacher.response.delta":
		return preview, preview.Sequence > 1 && preview.Text != "" && utf8.RuneCountInString(preview.Text) <= teacherPreviewMaxRunes
	case "teacher.response.completed", "teacher.response.failed", "teacher.response.cancelled":
		preview.terminal = true
		return preview, preview.Sequence > 1 && preview.Text == ""
	default:
		return preview, false
	}
}

// BroadcastTeacherResponse routes previews only to the originating learner and
// checks that the source message still belongs to a public-capability dialog.
func (h *Hub) BroadcastTeacherResponse(payload []byte) {
	now := time.Now().UTC()
	preview, ok := decodeTeacherResponsePreview(payload, now)
	if !ok {
		return
	}

	h.mu.RLock()
	prior := h.teacherPreviews[preview.DialogID]
	h.mu.RUnlock()
	if !preview.follows(prior) {
		return
	}
	preview.policy = prior.policy
	if prior.AttemptID != preview.AttemptID {
		policy, ok := h.teacherMessagePolicy(preview)
		if !ok {
			return
		}
		preview.policy = policy
	}
	if preview.Text != "" && domain.AnalyzeMessageContent(preview.Text, 0, 0).Validate(preview.policy) != nil {
		return
	}
	frame, err := json.Marshal(preview.teacherResponseFrame)
	if err != nil {
		return
	}
	h.mu.Lock()
	if h.closed || !preview.follows(h.teacherPreviews[preview.DialogID]) {
		h.mu.Unlock()
		return
	}
	if len(h.teacherPreviews) >= teacherPreviewCapacity {
		h.trimTeacherPreviews(now, preview.DialogID)
	}
	h.teacherPreviews[preview.DialogID] = preview
	if h.teacherPreviewExpiry == nil {
		h.teacherPreviewExpiry = time.AfterFunc(teacherPreviewLifetime, h.expireTeacherPreviews)
	}
	var slow []*client
	for item := range h.dialogs[preview.DialogID] {
		if item.session.UserID == preview.StudentID && !item.push(frame) {
			slow = append(slow, item)
		}
	}
	h.mu.Unlock()
	for _, item := range slow {
		h.unregister(item)
	}
}

func (h *Hub) teacherMessagePolicy(preview teacherResponsePreview) (domain.Policy, bool) {
	if visibility, ok := h.Visibility.(interface {
		TeacherPreviewPolicy(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64) (domain.Policy, error)
	}); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		policy, err := visibility.TeacherPreviewPolicy(ctx, preview.DialogID, preview.StudentID, preview.MessageID, preview.MessageSequence)
		return policy, err == nil && ctx.Err() == nil
	}
	return domain.DefaultPolicy(), h.Visibility == nil
}

func (h *Hub) expireTeacherPreviews() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.teacherPreviewExpiry = nil
	if h.closed {
		return
	}
	now := time.Now()
	var earliest time.Time
	for id, preview := range h.teacherPreviews {
		expires := preview.OccurredAt.Add(teacherPreviewLifetime)
		if !expires.After(now) {
			delete(h.teacherPreviews, id)
			continue
		}
		if earliest.IsZero() || expires.Before(earliest) {
			earliest = expires
		}
	}
	if !earliest.IsZero() {
		h.teacherPreviewExpiry = time.AfterFunc(time.Until(earliest), h.expireTeacherPreviews)
	}
}

func (h *Hub) trimTeacherPreviews(now time.Time, dialogID uuid.UUID) {
	var oldest uuid.UUID
	var oldestAt time.Time
	for id, prior := range h.teacherPreviews {
		if now.Sub(prior.OccurredAt) >= teacherPreviewLifetime {
			delete(h.teacherPreviews, id)
			continue
		}
		if oldest == uuid.Nil || prior.OccurredAt.Before(oldestAt) {
			oldest, oldestAt = id, prior.OccurredAt
		}
	}
	if _, present := h.teacherPreviews[dialogID]; !present && len(h.teacherPreviews) >= teacherPreviewCapacity {
		delete(h.teacherPreviews, oldest)
	}
}

func (h *Hub) activateWithTeacherPreview(item *client, ready []byte) [][]byte {
	now := time.Now()
	h.mu.RLock()
	defer h.mu.RUnlock()
	initial := [][]byte{ready}
	for dialogID := range item.dialogs {
		preview, ok := h.teacherPreviews[dialogID]
		if ok && !preview.terminal && preview.StudentID == item.session.UserID &&
			now.Sub(preview.OccurredAt) < teacherPreviewLifetime {
			frame, err := json.Marshal(preview.teacherResponseFrame)
			if err == nil {
				initial = append(initial, frame)
			}
		}
	}
	return h.activate(item, initial...)
}
