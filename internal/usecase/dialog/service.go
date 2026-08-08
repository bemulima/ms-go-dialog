package dialog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

type ParticipantResolver interface {
	RequireActiveUsers(ctx context.Context, ids []uuid.UUID) error
}

type Service struct {
	Spaces       repository.SpaceRepository
	Dialogs      repository.DialogRepository
	Members      repository.MemberRepository
	Blocks       repository.BlockRepository
	Outbox       repository.OutboxRepository
	Tx           repository.TransactionManager
	Participants ParticipantResolver
	Now          func() time.Time
	NewID        func() uuid.UUID
}

type EnsurePersonalInput struct {
	SpaceKey      string
	ParticipantID uuid.UUID
}

type CreateGroupInput struct {
	SpaceKey       string
	Title          string
	ParticipantIDs []uuid.UUID
}

type AddMemberInput struct {
	DialogID uuid.UUID
	UserID   uuid.UUID
}

type ChangeRoleInput struct {
	DialogID uuid.UUID
	UserID   uuid.UUID
	Role     domain.MemberRole
}

type UpdateGroupInput struct {
	DialogID        uuid.UUID
	Title           string
	ExpectedVersion int
}

type View struct {
	Dialog        domain.Dialog
	Space         domain.Space
	Members       []domain.Member
	CurrentMember domain.Member
}

func (s Service) BlockUser(ctx context.Context, actor domain.Actor, userID uuid.UUID) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	block := domain.UserBlock{BlockerID: actor.UserID, BlockedID: userID, CreatedAt: s.now()}
	if err := block.Validate(); err != nil {
		return err
	}
	if s.Blocks == nil {
		return fmt.Errorf("block repository is not configured")
	}
	return s.Blocks.Block(ctx, actor.UserID, userID)
}
func (s Service) UnblockUser(ctx context.Context, actor domain.Actor, userID uuid.UUID) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if userID == uuid.Nil || userID == actor.UserID {
		return domain.ErrValidation
	}
	if s.Blocks == nil {
		return fmt.Errorf("block repository is not configured")
	}
	return s.Blocks.Unblock(ctx, actor.UserID, userID)
}

type EnsureResult struct {
	View    View
	Created bool
}

func (s Service) EnsurePersonal(ctx context.Context, actor domain.Actor, in EnsurePersonalInput) (EnsureResult, error) {
	if err := actor.Validate(); err != nil {
		return EnsureResult{}, err
	}
	if in.ParticipantID == uuid.Nil || in.ParticipantID == actor.UserID {
		return EnsureResult{}, fmt.Errorf("%w: a different participant is required", domain.ErrValidation)
	}
	space, err := s.activeSpace(ctx, in.SpaceKey)
	if err != nil {
		return EnsureResult{}, err
	}
	if !space.Policy.AllowPersonal {
		return EnsureResult{}, domain.ErrForbidden
	}
	if s.Participants != nil {
		if err := s.Participants.RequireActiveUsers(ctx, []uuid.UUID{in.ParticipantID}); err != nil {
			return EnsureResult{}, err
		}
	}
	if s.Blocks != nil {
		blocked, err := s.Blocks.ExistsEitherDirection(ctx, actor.UserID, in.ParticipantID)
		if err != nil {
			return EnsureResult{}, err
		}
		if blocked {
			return EnsureResult{}, domain.ErrBlocked
		}
	}
	key, err := domain.BuildPersonalKey(actor.UserID, in.ParticipantID)
	if err != nil {
		return EnsureResult{}, err
	}

	var result EnsureResult
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.Dialogs.LockPersonalKey(txCtx, space.ID, key[:]); err != nil {
			return err
		}
		existing, err := s.Dialogs.FindPersonalByKey(txCtx, space.ID, key[:])
		if err == nil {
			view, err := s.buildView(txCtx, actor.UserID, space, existing)
			if err != nil {
				return err
			}
			result = EnsureResult{View: view}
			return nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		now, dialogID := s.now(), s.newID()
		item := domain.Dialog{
			ID: dialogID, SpaceID: space.ID, Type: domain.DialogTypePersonal, Status: domain.DialogStatusActive,
			PersonalKey: append([]byte(nil), key[:]...), CreatedBy: actor.UserID, Version: 1,
			MemberCount: 2, MaxEventSequence: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Dialogs.Create(txCtx, item); err != nil {
			return err
		}
		members := []domain.Member{
			newMember(item.ID, actor.UserID, domain.MemberRoleMember, actor.UserID, 1, now),
			newMember(item.ID, in.ParticipantID, domain.MemberRoleMember, actor.UserID, 1, now),
		}
		if err := s.Members.CreateMany(txCtx, members); err != nil {
			return err
		}
		if err := s.addCreatedEvent(txCtx, item, members, now); err != nil {
			return err
		}
		result = EnsureResult{Created: true, View: View{Dialog: item, Space: space, Members: members, CurrentMember: members[0]}}
		return nil
	})
	return result, err
}

func (s Service) CreateGroup(ctx context.Context, actor domain.Actor, in CreateGroupInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	space, err := s.activeSpace(ctx, in.SpaceKey)
	if err != nil {
		return View{}, err
	}
	if !space.Policy.AllowGroups {
		return View{}, domain.ErrForbidden
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len([]rune(title)) > 200 {
		return View{}, fmt.Errorf("%w: group title must contain 1 to 200 characters", domain.ErrValidation)
	}
	participants, err := uniqueParticipants(actor.UserID, in.ParticipantIDs)
	if err != nil {
		return View{}, err
	}
	if len(participants) > space.Policy.MaxGroupMembers {
		return View{}, domain.ErrMemberLimit
	}
	if s.Participants != nil {
		if err := s.Participants.RequireActiveUsers(ctx, participants); err != nil {
			return View{}, err
		}
	}

	var view View
	err = s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		now := s.now()
		item := domain.Dialog{
			ID: s.newID(), SpaceID: space.ID, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
			Title: title, CreatedBy: actor.UserID, Version: 1, MemberCount: len(participants),
			MaxEventSequence: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := item.Validate(); err != nil {
			return err
		}
		if err := s.Dialogs.Create(txCtx, item); err != nil {
			return err
		}
		members := make([]domain.Member, 0, len(participants))
		for _, participantID := range participants {
			role := domain.MemberRoleMember
			if participantID == actor.UserID {
				role = domain.MemberRoleOwner
			}
			members = append(members, newMember(item.ID, participantID, role, actor.UserID, 1, now))
		}
		if err := s.Members.CreateMany(txCtx, members); err != nil {
			return err
		}
		if err := s.addCreatedEvent(txCtx, item, members, now); err != nil {
			return err
		}
		current := members[0]
		for _, member := range members {
			if member.UserID == actor.UserID {
				current = member
				break
			}
		}
		view = View{Dialog: item, Space: space, Members: members, CurrentMember: current}
		return nil
	})
	return view, err
}

func (s Service) Get(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	item, err := s.Dialogs.GetByID(ctx, dialogID)
	if err != nil {
		return View{}, mapNotFound(err, domain.ErrDialogNotFound)
	}
	space, err := s.Spaces.GetByID(ctx, item.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive || item.Status == domain.DialogStatusHidden {
		return View{}, domain.ErrDialogNotFound
	}
	return s.buildView(ctx, actor.UserID, space, item)
}

func (s Service) UpdateGroup(ctx context.Context, actor domain.Actor, in UpdateGroupInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	title := strings.TrimSpace(in.Title)
	if in.DialogID == uuid.Nil || in.ExpectedVersion < 1 || title == "" || len([]rune(title)) > 200 {
		return View{}, domain.ErrValidation
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, space, current, err := s.loadGroupForManagement(txCtx, actor, in.DialogID)
		if err != nil {
			return err
		}
		if current.Role != domain.MemberRoleOwner && current.Role != domain.MemberRoleAdmin {
			return domain.ErrForbidden
		}
		if item.Version != in.ExpectedVersion {
			return domain.ErrMessageConflict
		}
		if item.Title == title {
			result, err = s.buildView(txCtx, actor.UserID, space, item)
			return err
		}
		now := s.now()
		item.Title = title
		item.MaxEventSequence++
		item.Version++
		item.UpdatedAt = now
		if err := s.Dialogs.UpdateState(txCtx, item, in.ExpectedVersion); err != nil {
			return err
		}
		eventID := s.newID()
		payload, err := json.Marshal(map[string]any{"schema_version": 1, "event_id": eventID, "occurred_at": now, "dialog_id": item.ID, "space_id": item.SpaceID, "event_sequence": item.MaxEventSequence, "title": item.Title, "version": item.Version, "actor_id": actor.UserID})
		if err != nil {
			return err
		}
		if err = s.Outbox.Add(txCtx, domain.OutboxEvent{ID: eventID, DialogID: item.ID, AggregateType: "dialog", AggregateID: item.ID, Subject: domain.EventDialogUpdated, EventSequence: item.MaxEventSequence, SchemaVersion: 1, Payload: payload, NextAttemptAt: now, CreatedAt: now}); err != nil {
			return err
		}
		result, err = s.buildView(txCtx, actor.UserID, space, item)
		return err
	})
	return result, err
}

func (s Service) List(ctx context.Context, actor domain.Actor, query repository.DialogListQuery) ([]repository.DialogListItem, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > MaxPageLimit+1 {
		return nil, fmt.Errorf("%w: invalid page limit", domain.ErrValidation)
	}
	query.UserID = actor.UserID
	return s.Dialogs.ListForUser(ctx, query)
}

func (s Service) AddMember(ctx context.Context, actor domain.Actor, in AddMemberInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	if in.DialogID == uuid.Nil || in.UserID == uuid.Nil {
		return View{}, fmt.Errorf("%w: dialog and user UUIDs are required", domain.ErrValidation)
	}
	if s.Participants != nil {
		if err := s.authorizeGroupManager(ctx, actor, in.DialogID); err != nil {
			return View{}, err
		}
		if err := s.Participants.RequireActiveUsers(ctx, []uuid.UUID{in.UserID}); err != nil {
			return View{}, err
		}
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, space, current, err := s.loadGroupForManagement(txCtx, actor, in.DialogID)
		if err != nil {
			return err
		}
		if current.Role != domain.MemberRoleOwner && current.Role != domain.MemberRoleAdmin {
			return domain.ErrForbidden
		}
		existing, err := s.Members.GetForUpdate(txCtx, item.ID, in.UserID)
		if err == nil && existing.Status == domain.MemberStatusActive {
			return domain.ErrAlreadyExists
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if item.MemberCount >= space.Policy.MaxGroupMembers {
			return domain.ErrMemberLimit
		}
		now, eventSequence := s.now(), item.MaxEventSequence+1
		member := newMember(item.ID, in.UserID, domain.MemberRoleMember, actor.UserID, eventSequence, now)
		member.HistoryFromMessageSequence = item.MaxMessageSequence + 1
		member.LastReadMessageSequence = item.MaxMessageSequence
		if err == nil {
			if err := s.Members.Update(txCtx, member); err != nil {
				return err
			}
		} else if err := s.Members.Create(txCtx, member); err != nil {
			return err
		}
		item.MemberCount++
		item.MaxEventSequence, item.Version, item.UpdatedAt = eventSequence, item.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, item, item.Version-1); err != nil {
			return err
		}
		if err := s.addMemberEvent(txCtx, item, member, domain.EventDialogMemberAdded, actor.UserID, now); err != nil {
			return err
		}
		result, err = s.buildView(txCtx, actor.UserID, space, item)
		return err
	})
	return result, err
}

func (s Service) RemoveMember(ctx context.Context, actor domain.Actor, dialogID, userID uuid.UUID) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	if dialogID == uuid.Nil || userID == uuid.Nil {
		return View{}, domain.ErrValidation
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, space, current, err := s.loadGroupForManagement(txCtx, actor, dialogID)
		if err != nil {
			return err
		}
		if current.Role != domain.MemberRoleOwner && current.Role != domain.MemberRoleAdmin {
			return domain.ErrForbidden
		}
		target, err := s.Members.GetForUpdate(txCtx, dialogID, userID)
		if err != nil || target.Status != domain.MemberStatusActive {
			return domain.ErrMemberNotFound
		}
		if target.Role == domain.MemberRoleOwner || target.UserID == actor.UserID {
			return domain.ErrForbidden
		}
		now, eventSequence := s.now(), item.MaxEventSequence+1
		target.Status, target.LeftAt, target.LastEventSequence, target.UpdatedAt = domain.MemberStatusRemoved, &now, eventSequence, now
		if err := s.Members.Update(txCtx, target); err != nil {
			return err
		}
		item.MemberCount--
		item.MaxEventSequence, item.Version, item.UpdatedAt = eventSequence, item.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, item, item.Version-1); err != nil {
			return err
		}
		if err := s.addMemberEvent(txCtx, item, target, domain.EventDialogMemberRemoved, actor.UserID, now); err != nil {
			return err
		}
		result, err = s.buildView(txCtx, actor.UserID, space, item)
		return err
	})
	return result, err
}

func (s Service) Leave(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if dialogID == uuid.Nil {
		return domain.ErrValidation
	}
	return s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, _, current, err := s.loadGroupForManagement(txCtx, actor, dialogID)
		if err != nil {
			return err
		}
		if current.Role == domain.MemberRoleOwner {
			owners, err := s.Members.CountActiveOwners(txCtx, dialogID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return domain.ErrLastOwner
			}
		}
		now, eventSequence := s.now(), item.MaxEventSequence+1
		current.Status, current.LeftAt, current.LastEventSequence, current.UpdatedAt = domain.MemberStatusLeft, &now, eventSequence, now
		if err := s.Members.Update(txCtx, current); err != nil {
			return err
		}
		item.MemberCount--
		item.MaxEventSequence, item.Version, item.UpdatedAt = eventSequence, item.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, item, item.Version-1); err != nil {
			return err
		}
		return s.addMemberEvent(txCtx, item, current, domain.EventDialogMemberRemoved, actor.UserID, now)
	})
}

func (s Service) ChangeRole(ctx context.Context, actor domain.Actor, in ChangeRoleInput) (View, error) {
	if err := actor.Validate(); err != nil {
		return View{}, err
	}
	if in.DialogID == uuid.Nil || in.UserID == uuid.Nil || in.Role < domain.MemberRoleOwner || in.Role > domain.MemberRoleMember {
		return View{}, fmt.Errorf("%w: invalid member role", domain.ErrValidation)
	}
	var result View
	err := s.Tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		item, space, current, err := s.loadGroupForManagement(txCtx, actor, in.DialogID)
		if err != nil {
			return err
		}
		if current.Role != domain.MemberRoleOwner {
			return domain.ErrForbidden
		}
		target, err := s.Members.GetForUpdate(txCtx, in.DialogID, in.UserID)
		if err != nil || target.Status != domain.MemberStatusActive {
			return domain.ErrMemberNotFound
		}
		if target.Role == domain.MemberRoleOwner && in.Role != domain.MemberRoleOwner {
			owners, err := s.Members.CountActiveOwners(txCtx, in.DialogID)
			if err != nil {
				return err
			}
			if owners <= 1 {
				return domain.ErrLastOwner
			}
		}
		if target.Role == in.Role {
			result, err = s.buildView(txCtx, actor.UserID, space, item)
			return err
		}
		now, eventSequence := s.now(), item.MaxEventSequence+1
		target.Role, target.LastEventSequence, target.UpdatedAt = in.Role, eventSequence, now
		if err := s.Members.Update(txCtx, target); err != nil {
			return err
		}
		item.MaxEventSequence, item.Version, item.UpdatedAt = eventSequence, item.Version+1, now
		if err := s.Dialogs.UpdateState(txCtx, item, item.Version-1); err != nil {
			return err
		}
		if err := s.addMemberEvent(txCtx, item, target, domain.EventDialogMemberRoleUpdated, actor.UserID, now); err != nil {
			return err
		}
		result, err = s.buildView(txCtx, actor.UserID, space, item)
		return err
	})
	return result, err
}

func (s Service) authorizeGroupManager(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) error {
	item, err := s.Dialogs.GetByID(ctx, dialogID)
	if err != nil || item.Status == domain.DialogStatusHidden {
		return domain.ErrDialogNotFound
	}
	if item.Type != domain.DialogTypeGroup || item.Status != domain.DialogStatusActive {
		return domain.ErrForbidden
	}
	space, err := s.Spaces.GetByID(ctx, item.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.ErrDialogNotFound
	}
	current, err := s.Members.Get(ctx, dialogID, actor.UserID)
	if err != nil || current.Status != domain.MemberStatusActive || (current.Role != domain.MemberRoleOwner && current.Role != domain.MemberRoleAdmin) {
		return domain.ErrForbidden
	}
	return nil
}

func (s Service) loadGroupForManagement(ctx context.Context, actor domain.Actor, dialogID uuid.UUID) (domain.Dialog, domain.Space, domain.Member, error) {
	item, err := s.Dialogs.GetByIDForUpdate(ctx, dialogID)
	if err != nil || item.Status == domain.DialogStatusHidden {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	if item.Type != domain.DialogTypeGroup || item.Status != domain.DialogStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrForbidden
	}
	space, err := s.Spaces.GetByID(ctx, item.SpaceID)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrDialogNotFound
	}
	current, err := s.Members.GetForUpdate(ctx, dialogID, actor.UserID)
	if err != nil || current.Status != domain.MemberStatusActive {
		return domain.Dialog{}, domain.Space{}, domain.Member{}, domain.ErrForbidden
	}
	return item, space, current, nil
}

func (s Service) addMemberEvent(ctx context.Context, item domain.Dialog, member domain.Member, subject domain.EventSubject, actorID uuid.UUID, now time.Time) error {
	eventID := s.newID()
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "event_id": eventID, "occurred_at": now,
		"dialog_id": item.ID, "event_sequence": item.MaxEventSequence,
		"space_id": item.SpaceID, "user_id": member.UserID, "role": member.Role, "status": member.Status, "actor_id": actorID,
	})
	if err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: eventID, DialogID: item.ID, AggregateType: "member", AggregateID: member.UserID,
		Subject: subject, EventSequence: item.MaxEventSequence, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: now, CreatedAt: now,
	})
}

func (s Service) activeSpace(ctx context.Context, key string) (domain.Space, error) {
	if !spaceKeyValid(key) {
		return domain.Space{}, fmt.Errorf("%w: invalid space key", domain.ErrValidation)
	}
	space, err := s.Spaces.GetByKey(ctx, key)
	if err != nil || space.Status != domain.SpaceStatusActive {
		return domain.Space{}, domain.ErrDialogNotFound
	}
	if err := space.Validate(); err != nil {
		return domain.Space{}, err
	}
	return space, nil
}

func (s Service) buildView(ctx context.Context, userID uuid.UUID, space domain.Space, item domain.Dialog) (View, error) {
	current, err := s.Members.Get(ctx, item.ID, userID)
	if err != nil || current.Status != domain.MemberStatusActive {
		return View{}, domain.ErrForbidden
	}
	members, err := s.Members.ListActive(ctx, item.ID)
	if err != nil {
		return View{}, err
	}
	return View{Dialog: item, Space: space, Members: members, CurrentMember: current}, nil
}

func (s Service) addCreatedEvent(ctx context.Context, item domain.Dialog, members []domain.Member, now time.Time) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	payload, err := json.Marshal(map[string]any{
		"schema_version": 1, "event_id": s.newID(), "occurred_at": now,
		"dialog_id": item.ID, "event_sequence": int64(1), "type": item.Type,
		"space_id": item.SpaceID, "created_by": item.CreatedBy, "participant_ids": ids,
	})
	if err != nil {
		return err
	}
	var envelope struct {
		EventID uuid.UUID `json:"event_id"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	return s.Outbox.Add(ctx, domain.OutboxEvent{
		ID: envelope.EventID, DialogID: item.ID, AggregateType: "dialog", AggregateID: item.ID,
		Subject: domain.EventDialogCreated, EventSequence: 1, SchemaVersion: 1,
		Payload: payload, NextAttemptAt: now, CreatedAt: now,
	})
}

func uniqueParticipants(actorID uuid.UUID, input []uuid.UUID) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]struct{}{actorID: {}}
	result := []uuid.UUID{actorID}
	for _, id := range input {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%w: participant UUID is required", domain.ErrValidation)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) < 2 {
		return nil, fmt.Errorf("%w: a group requires at least two participants", domain.ErrValidation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, nil
}

func newMember(dialogID, userID uuid.UUID, role domain.MemberRole, addedBy uuid.UUID, eventSequence int64, now time.Time) domain.Member {
	return domain.Member{
		DialogID: dialogID, UserID: userID, Role: role, Status: domain.MemberStatusActive,
		LastEventSequence: eventSequence, AddedBy: addedBy, JoinedAt: now, UpdatedAt: now,
	}
}

func mapNotFound(err, target error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return target
	}
	return err
}

func spaceKeyValid(key string) bool {
	if len(key) < 2 || len(key) > 64 {
		return false
	}
	for index, char := range key {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || (index > 0 && (char == '.' || char == '_' || char == '-')) {
			continue
		}
		return false
	}
	return true
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) newID() uuid.UUID {
	if s.NewID != nil {
		return s.NewID()
	}
	return uuid.New()
}
