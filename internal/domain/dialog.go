package domain

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DialogType int16

const (
	DialogTypePersonal DialogType = iota + 1
	DialogTypeGroup
)

type DialogStatus int16

const (
	DialogStatusActive DialogStatus = iota + 1
	DialogStatusClosed
	DialogStatusHidden
)

type MemberRole int16

const (
	MemberRoleOwner MemberRole = iota + 1
	MemberRoleAdmin
	MemberRoleMember
)

type MemberStatus int16

const (
	MemberStatusActive MemberStatus = iota + 1
	MemberStatusLeft
	MemberStatusRemoved
)

type Dialog struct {
	ID                 uuid.UUID
	SpaceID            uuid.UUID
	Type               DialogType
	Status             DialogStatus
	PersonalKey        []byte
	Title              string
	CreatedBy          uuid.UUID
	Version            int
	MemberCount        int
	MessageCount       int64
	MaxMessageSequence int64
	MaxEventSequence   int64
	LastMessageID      *uuid.UUID
	LastMessageAt      *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func BuildPersonalKey(first, second uuid.UUID) ([32]byte, error) {
	if first == uuid.Nil || second == uuid.Nil || first == second {
		return [32]byte{}, fmt.Errorf("%w: two different users are required", ErrValidation)
	}
	a, b := first[:], second[:]
	if bytes.Compare(a, b) > 0 {
		a, b = b, a
	}
	payload := make([]byte, 0, 32)
	payload = append(payload, a...)
	payload = append(payload, b...)
	return sha256.Sum256(payload), nil
}

func (d Dialog) Validate() error {
	if d.ID == uuid.Nil || d.SpaceID == uuid.Nil || d.CreatedBy == uuid.Nil || d.Version < 1 ||
		d.MessageCount < 0 || d.MaxMessageSequence < 0 || d.MaxEventSequence < d.MaxMessageSequence {
		return fmt.Errorf("%w: invalid dialog identity or counters", ErrValidation)
	}
	if d.Status < DialogStatusActive || d.Status > DialogStatusHidden {
		return fmt.Errorf("%w: invalid dialog status", ErrValidation)
	}
	switch d.Type {
	case DialogTypePersonal:
		if len(d.PersonalKey) != sha256.Size || d.MemberCount != 2 || strings.TrimSpace(d.Title) != "" {
			return fmt.Errorf("%w: invalid personal dialog shape", ErrValidation)
		}
	case DialogTypeGroup:
		if len(d.PersonalKey) != 0 || d.MemberCount < 1 || d.MemberCount > HardMaxGroupMembers || len([]rune(strings.TrimSpace(d.Title))) > 200 || strings.TrimSpace(d.Title) == "" {
			return fmt.Errorf("%w: invalid group dialog shape", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: invalid dialog type", ErrValidation)
	}
	return nil
}

type Member struct {
	DialogID                   uuid.UUID
	UserID                     uuid.UUID
	Role                       MemberRole
	Status                     MemberStatus
	HistoryFromMessageSequence int64
	LastReadMessageSequence    int64
	UnreadCount                int64
	LastEventSequence          int64
	LastReadAt                 *time.Time
	MutedUntil                 *time.Time
	ArchivedAt                 *time.Time
	AddedBy                    uuid.UUID
	JoinedAt                   time.Time
	LeftAt                     *time.Time
	UpdatedAt                  time.Time
}

func (m Member) Validate() error {
	if m.DialogID == uuid.Nil || m.UserID == uuid.Nil || m.AddedBy == uuid.Nil ||
		m.Role < MemberRoleOwner || m.Role > MemberRoleMember ||
		m.Status < MemberStatusActive || m.Status > MemberStatusRemoved ||
		m.HistoryFromMessageSequence < 0 || m.LastReadMessageSequence < 0 || m.UnreadCount < 0 || m.LastEventSequence < 0 {
		return fmt.Errorf("%w: invalid member", ErrValidation)
	}
	if (m.Status == MemberStatusActive) != (m.LeftAt == nil) {
		return fmt.Errorf("%w: member lifecycle timestamp is inconsistent", ErrValidation)
	}
	return nil
}

func (m *Member) AdvanceRead(through, dialogMaximum, newlyRead int64, now time.Time) (bool, error) {
	if m.Status != MemberStatusActive {
		return false, ErrForbidden
	}
	if through < 0 || through > dialogMaximum || newlyRead < 0 {
		return false, ErrInvalidReadSequence
	}
	if through <= m.LastReadMessageSequence {
		return false, nil
	}
	m.LastReadMessageSequence = through
	if newlyRead >= m.UnreadCount {
		m.UnreadCount = 0
	} else {
		m.UnreadCount -= newlyRead
	}
	readAt := now.UTC()
	m.LastReadAt = &readAt
	m.UpdatedAt = readAt
	return true, nil
}

func (m *Member) MarkAllRead(dialogMaximum int64, now time.Time) (bool, error) {
	if dialogMaximum < 0 {
		return false, ErrInvalidReadSequence
	}
	changed := m.LastReadMessageSequence != dialogMaximum || m.UnreadCount != 0
	if !changed {
		return false, nil
	}
	m.LastReadMessageSequence = dialogMaximum
	m.UnreadCount = 0
	readAt := now.UTC()
	m.LastReadAt = &readAt
	m.UpdatedAt = readAt
	return true, nil
}
