package dialog

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type MentorOwnerProof struct {
	Kind                                                 domain.MentorContextKind
	SourceID, ActorID, MentorID, StudentID, MembershipID uuid.UUID
	Revision, HistoryFromRevision                        int64
	Authors                                              map[uuid.UUID]uuid.UUID
}
type MentorContextOwner interface {
	ReadAccess(context.Context, domain.MentorContextKind, uuid.UUID, uuid.UUID, []uuid.UUID) (MentorOwnerProof, error)
}
type MentorEducationService struct {
	Enabled  bool
	SpaceKey string
	Dialogs  *Service
	Bindings repository.MentorRepository
	Owner    MentorContextOwner
}
type MentorEnsureInput struct {
	Kind domain.MentorContextKind `json:"kind"`
	ID   uuid.UUID                `json:"id"`
}
type MentorEnsured struct {
	Schema    string                   `json:"schema"`
	DialogID  uuid.UUID                `json:"dialog_id"`
	BindingID uuid.UUID                `json:"mentor_binding_id"`
	Kind      domain.MentorContextKind `json:"kind"`
	SourceID  uuid.UUID                `json:"source_id"`
}

func (s *MentorEducationService) Protected(ctx context.Context, id uuid.UUID) (bool, error) {
	if s == nil || s.Bindings == nil {
		return false, domain.ErrDependencyUnavailable
	}
	return s.Bindings.Protected(ctx, id)
}
func (s *MentorEducationService) ready() error {
	if s == nil || !s.Enabled {
		return domain.ErrFeatureDisabled
	}
	if s.Owner == nil || s.Bindings == nil || s.Dialogs == nil || s.Dialogs.Blocks == nil || s.Dialogs.Participants == nil || s.SpaceKey == "" {
		return domain.ErrDependencyUnavailable
	}
	return nil
}
func (s *MentorEducationService) owner(ctx context.Context, kind domain.MentorContextKind, id, actor uuid.UUID, authors []uuid.UUID) (MentorOwnerProof, error) {
	p, err := s.Owner.ReadAccess(ctx, kind, id, actor, authors)
	if err != nil {
		return p, err
	}
	if p.Kind != kind || p.SourceID != id || p.ActorID != actor || p.MentorID == uuid.Nil || p.MembershipID == uuid.Nil || p.Revision < 1 || p.HistoryFromRevision < 1 || p.HistoryFromRevision > p.Revision || (kind == domain.MentorAssignment && (p.StudentID == uuid.Nil || actor != p.StudentID && actor != p.MentorID)) || (kind == domain.MentorCohort && p.StudentID != uuid.Nil) {
		return p, domain.ErrDependencyUnavailable
	}
	peer := p.MentorID
	if actor == peer {
		peer = p.StudentID
	}
	if peer != uuid.Nil {
		blocked, err := s.Dialogs.Blocks.ExistsEitherDirection(ctx, actor, peer)
		if err != nil {
			return p, err
		}
		if blocked {
			return p, domain.ErrBlocked
		}
	}
	for author := range p.Authors {
		if author == actor {
			continue
		}
		blocked, err := s.Dialogs.Blocks.ExistsEitherDirection(ctx, actor, author)
		if err != nil {
			return p, err
		}
		if blocked {
			delete(p.Authors, author)
		}
	}
	return p, nil
}
func (s *MentorEducationService) Check(ctx context.Context, actor, dialogID uuid.UUID, authors []uuid.UUID) (*domain.MentorAccess, error) {
	id := domain.SelectedMentorBinding(ctx)
	if id == nil {
		protected, err := s.Protected(ctx, dialogID)
		if err != nil {
			return nil, err
		}
		if protected {
			return nil, domain.ErrDialogNotFound
		}
		return nil, nil
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	b, err := s.Bindings.Get(ctx, *id)
	if err != nil || !b.Valid() || b.DialogID != dialogID {
		return nil, domain.ErrDialogNotFound
	}
	p, err := s.owner(ctx, b.Kind, b.SourceID, actor, authors)
	if err != nil {
		return nil, err
	}
	if p.MentorID != b.MentorID || p.StudentID != b.StudentID {
		return nil, domain.ErrForbidden
	}
	return &domain.MentorAccess{Binding: b, ActorID: actor, MembershipID: p.MembershipID, Revision: p.Revision, HistoryFromRevision: p.HistoryFromRevision, Authors: p.Authors}, nil
}
func (s *MentorEducationService) Ensure(ctx context.Context, actor domain.Actor, in MentorEnsureInput) (MentorEnsured, error) {
	if err := actor.Validate(); err != nil {
		return MentorEnsured{}, err
	}
	if err := s.ready(); err != nil {
		return MentorEnsured{}, err
	}
	if !in.Kind.Valid() || in.ID == uuid.Nil {
		return MentorEnsured{}, domain.ErrValidation
	}
	p, err := s.owner(ctx, in.Kind, in.ID, actor.UserID, nil)
	if err != nil {
		return MentorEnsured{}, err
	}
	participants := []uuid.UUID{actor.UserID}
	if p.MentorID != actor.UserID {
		participants = append(participants, p.MentorID)
	}
	if err = s.Dialogs.Participants.RequireActiveUsers(ctx, participants); err != nil {
		return MentorEnsured{}, err
	}
	d := s.Dialogs
	space, err := d.activeSpace(ctx, s.SpaceKey)
	if err != nil {
		return MentorEnsured{}, err
	}
	var b domain.MentorBinding
	if in.Kind == domain.MentorAssignment {
		contact := MentorContactService{Dialogs: d, Enabled: true, SpaceKey: s.SpaceKey}
		pair, err := contact.Ensure(ctx, MentorContactInput{StudentID: p.StudentID, MentorID: p.MentorID})
		if err != nil {
			return MentorEnsured{}, err
		}
		b = domain.MentorBinding{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("mentor:ASSIGNMENT:"+in.ID.String())), DialogID: pair.DialogID, Kind: in.Kind, SourceID: in.ID, MentorID: p.MentorID, StudentID: p.StudentID}
	}
	err = d.Tx.WithinTransaction(ctx, func(tx context.Context) error {
		if err := s.Bindings.Lock(tx, in.Kind, in.ID); err != nil {
			return err
		}
		existing, err := s.Bindings.Find(tx, in.Kind, in.ID)
		if err == nil {
			if existing.MentorID != p.MentorID || existing.StudentID != p.StudentID || in.Kind == domain.MentorAssignment && existing.DialogID != b.DialogID {
				return domain.ErrForbidden
			}
			b = existing
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		} else {
			if in.Kind == domain.MentorCohort {
				if !space.Policy.AllowGroups {
					return domain.ErrForbidden
				}
				now := d.now()
				group := domain.Dialog{ID: d.newID(), SpaceID: space.ID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Mentor cohort", CreatedBy: p.MentorID, Version: 1, MemberCount: 1, MaxEventSequence: 1, CreatedAt: now, UpdatedAt: now}
				if err := group.Validate(); err != nil {
					return err
				}
				if err := d.Dialogs.Create(tx, group); err != nil {
					return err
				}
				if err := d.Members.Create(tx, newMember(group.ID, p.MentorID, domain.MemberRoleOwner, p.MentorID, 1, now)); err != nil {
					return err
				}
				b = domain.MentorBinding{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("mentor:COHORT:"+in.ID.String())), DialogID: group.ID, Kind: in.Kind, SourceID: in.ID, MentorID: p.MentorID}
			}
			if err := s.Bindings.Put(tx, b); err != nil {
				return err
			}
		}
		group, err := d.Dialogs.GetByIDForUpdate(tx, b.DialogID)
		if err != nil || group.Status != domain.DialogStatusActive {
			return domain.ErrDialogNotFound
		}
		return nil
	})
	if err != nil {
		return MentorEnsured{}, err
	}
	current, err := s.Check(domain.WithMentorBinding(ctx, b.ID), actor.UserID, b.DialogID, nil)
	if err != nil {
		return MentorEnsured{}, err
	}
	if current.Revision != p.Revision || current.MembershipID != p.MembershipID || current.HistoryFromRevision != p.HistoryFromRevision {
		return MentorEnsured{}, domain.ErrForbidden
	}
	return MentorEnsured{Schema: "mentor-dialog.context.v1", DialogID: b.DialogID, BindingID: b.ID, Kind: b.Kind, SourceID: b.SourceID}, nil
}
