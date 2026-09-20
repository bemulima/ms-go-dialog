package websocket

import (
	"encoding/json"
	"fmt"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/google/uuid"
	"time"
)

const protocolVersion = 1

var websocketTypes = map[domain.EventSubject]string{domain.EventDialogCreated: "dialog.created", domain.EventDialogUpdated: "dialog.updated", domain.EventDialogClosed: "dialog.closed", domain.EventDialogMemberAdded: "member.added", domain.EventDialogMemberRemoved: "member.removed", domain.EventDialogMemberRoleUpdated: "member.role_updated", domain.EventDialogMessageCreated: "message.created", domain.EventDialogMessageUpdated: "message.updated", domain.EventDialogMessageDeleted: "message.deleted", domain.EventDialogMessageHidden: "message.hidden", domain.EventDialogMessageRestored: "message.restored", domain.EventDialogAttachmentReady: "attachment.ready", domain.EventDialogAttachmentFailed: "attachment.failed", domain.EventDialogReadUpdated: "read.updated"}

type routing struct {
	DialogID       uuid.UUID
	SpaceID        uuid.UUID
	UserID         uuid.UUID
	ParticipantIDs []uuid.UUID
}

func lifecycleEnvelope(subject domain.EventSubject, payload []byte) (routing, []byte, error) {
	eventType, ok := websocketTypes[subject]
	if !ok {
		return routing{}, nil, fmt.Errorf("unsupported lifecycle subject")
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(payload, &data); err != nil {
		return routing{}, nil, err
	}
	dialogID, err := rawUUID(data["dialog_id"])
	if err != nil {
		return routing{}, nil, err
	}
	eventID, err := rawUUID(data["event_id"])
	if err != nil {
		return routing{}, nil, err
	}
	var sequence int64
	var occurredAt time.Time
	if json.Unmarshal(data["event_sequence"], &sequence) != nil || sequence < 1 || json.Unmarshal(data["occurred_at"], &occurredAt) != nil || occurredAt.IsZero() {
		return routing{}, nil, fmt.Errorf("invalid lifecycle envelope")
	}
	route := routing{DialogID: dialogID}
	_ = json.Unmarshal(data["space_id"], &route.SpaceID)
	_ = json.Unmarshal(data["user_id"], &route.UserID)
	_ = json.Unmarshal(data["participant_ids"], &route.ParticipantIDs)
	delete(data, "schema_version")
	delete(data, "event_id")
	delete(data, "dialog_id")
	delete(data, "event_sequence")
	delete(data, "occurred_at")
	envelope := map[string]any{"v": protocolVersion, "type": eventType, "dialog_id": dialogID, "event_id": eventID, "event_sequence": sequence, "occurred_at": occurredAt, "data": data}
	encoded, err := json.Marshal(envelope)
	return route, encoded, err
}
func readyFrame(spaceID uuid.UUID, sequences map[uuid.UUID]int64, now time.Time) []byte {
	return mustJSON(map[string]any{"v": protocolVersion, "type": "connection.ready", "space_id": spaceID, "dialogs": sequences, "occurred_at": now})
}
func errorFrame(requestID, code, message string) []byte {
	return mustJSON(map[string]any{"v": protocolVersion, "type": "error", "request_id": requestID, "data": map[string]string{"error": code, "message": message}})
}
func pingFrame(now time.Time) []byte {
	return mustJSON(map[string]any{"v": protocolVersion, "type": "ping", "occurred_at": now})
}
func typingFrame(eventType string, dialogID, userID, eventID uuid.UUID, now time.Time) []byte {
	return mustJSON(map[string]any{"v": protocolVersion, "type": eventType, "event_id": eventID, "dialog_id": dialogID, "occurred_at": now, "data": map[string]any{"user_id": userID}})
}
func rawUUID(value json.RawMessage) (uuid.UUID, error) {
	var id uuid.UUID
	if len(value) == 0 || json.Unmarshal(value, &id) != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid UUID")
	}
	return id, nil
}
func mustJSON(value any) []byte { result, _ := json.Marshal(value); return result }
