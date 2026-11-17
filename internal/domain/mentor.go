package domain

import (
	"context"
	"github.com/google/uuid"
)

type MentorContextKind string

const (
	MentorAssignment MentorContextKind = "ASSIGNMENT"
	MentorCohort     MentorContextKind = "COHORT"
)

func (k MentorContextKind) Valid() bool { return k == MentorAssignment || k == MentorCohort }

// MentorBinding is a typed local reference to a foreign owner, never a grant.
type MentorBinding struct {
	ID, DialogID, SourceID, MentorID, StudentID uuid.UUID
	Kind                                        MentorContextKind
}

func (b MentorBinding) Valid() bool {
	return b.ID != uuid.Nil && b.DialogID != uuid.Nil && b.SourceID != uuid.Nil && b.MentorID != uuid.Nil && b.Kind.Valid() && ((b.Kind == MentorAssignment && b.StudentID != uuid.Nil && b.StudentID != b.MentorID) || (b.Kind == MentorCohort && b.StudentID == uuid.Nil))
}

type MentorAccess struct {
	Binding                       MentorBinding
	ActorID, MembershipID         uuid.UUID
	Revision, HistoryFromRevision int64
	Authors                       map[uuid.UUID]uuid.UUID
}

func (a MentorAccess) SameCurrent(b MentorAccess) bool {
	return a.Binding == b.Binding && a.ActorID == b.ActorID && a.MembershipID == b.MembershipID && a.Revision == b.Revision && a.HistoryFromRevision == b.HistoryFromRevision
}
func (a *MentorAccess) Allows(m Message) bool {
	if a == nil {
		return m.MentorBindingID == nil
	}
	if m.MentorBindingID == nil || *m.MentorBindingID != a.Binding.ID || m.DialogID != a.Binding.DialogID || m.MentorContextRevision < a.HistoryFromRevision {
		return false
	}
	return a.Authors[m.SenderID] == m.MentorAuthorMembershipID && m.MentorAuthorMembershipID != uuid.Nil
}

// CohortMember is a request-local projection of the foreign owner's current
// enrollment. Persisting a second roster would let historical leavers consume
// the ordinary group member cap and would create two membership authorities.
func (a *MentorAccess) CohortMember(d Dialog, user uuid.UUID) (Member, bool) {
	if a == nil || a.Binding.Kind != MentorCohort || a.Binding.DialogID != d.ID || a.ActorID != user || d.Type != DialogTypeGroup {
		return Member{}, false
	}
	role := MemberRoleMember
	if user == a.Binding.MentorID {
		role = MemberRoleOwner
	}
	return Member{DialogID: d.ID, UserID: user, Role: role, Status: MemberStatusActive, HistoryFromMessageSequence: 1, AddedBy: a.Binding.MentorID, JoinedAt: d.CreatedAt, UpdatedAt: d.CreatedAt}, true
}

type MentorAccessGuard interface {
	Check(context.Context, uuid.UUID, uuid.UUID, []uuid.UUID) (*MentorAccess, error)
	Protected(context.Context, uuid.UUID) (bool, error)
}
type mentorSelectionKey struct{}
type mentorProofKey struct{}

func WithMentorBinding(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, mentorSelectionKey{}, id)
}
func SelectedMentorBinding(ctx context.Context) *uuid.UUID {
	if id, ok := ctx.Value(mentorSelectionKey{}).(uuid.UUID); ok && id != uuid.Nil {
		return &id
	}
	return nil
}
func WithMentorAccess(ctx context.Context, a *MentorAccess) context.Context {
	if a == nil {
		return ctx
	}
	return context.WithValue(ctx, mentorProofKey{}, *a)
}
func CurrentMentorAccess(ctx context.Context) *MentorAccess {
	if a, ok := ctx.Value(mentorProofKey{}).(MentorAccess); ok {
		return &a
	}
	return nil
}

// CheckMentorRead is fail closed for explicit educational requests even when
// production wiring is disabled. Ordinary personal history needs no grant.
func CheckMentorRead(ctx context.Context, g MentorAccessGuard, user, dialog uuid.UUID, authors []uuid.UUID) (*MentorAccess, error) {
	if g == nil {
		if SelectedMentorBinding(ctx) != nil {
			return nil, ErrFeatureDisabled
		}
		return nil, nil
	}
	return g.Check(ctx, user, dialog, authors)
}
