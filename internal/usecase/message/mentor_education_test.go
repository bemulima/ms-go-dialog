package message

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"sort"
	"testing"
	"time"
)

type educationGuard struct {
	access  domain.MentorAccess
	calls   int
	denyAt  int
	authors map[uuid.UUID]uuid.UUID
	onCheck func(int)
}

func (g *educationGuard) Protected(_ context.Context, id uuid.UUID) (bool, error) {
	return g.access.Binding.Kind == domain.MentorCohort && g.access.Binding.DialogID == id, nil
}
func (g *educationGuard) Check(ctx context.Context, user, id uuid.UUID, authors []uuid.UUID) (*domain.MentorAccess, error) {
	g.calls++
	if g.onCheck != nil {
		g.onCheck(g.calls)
	}
	if g.denyAt > 0 && g.calls >= g.denyAt {
		return nil, domain.ErrForbidden
	}
	if domain.SelectedMentorBinding(ctx) == nil {
		if g.access.Binding.Kind == domain.MentorCohort {
			return nil, domain.ErrDialogNotFound
		}
		return nil, nil
	}
	a := g.access
	a.ActorID = user
	a.Authors = map[uuid.UUID]uuid.UUID{}
	for _, id := range authors {
		if membership, ok := g.authors[id]; ok {
			a.Authors[id] = membership
		}
	}
	return &a, nil
}

type educationMessages struct{ fakeMessages }

func (f *educationMessages) List(ctx context.Context, q repository.MessageListQuery) ([]domain.Message, error) {
	var rows []domain.Message
	for _, m := range f.items {
		if m.DialogID != q.DialogID || q.Before != nil && m.MessageSequence >= q.Before.Sequence || q.After != nil && m.MessageSequence <= q.After.Sequence {
			continue
		}
		rows = append(rows, m)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].MessageSequence < rows[j].MessageSequence })
	if len(rows) > q.Limit {
		if q.After != nil {
			rows = rows[:q.Limit]
		} else {
			rows = rows[len(rows)-q.Limit:]
		}
	}
	return rows, nil
}
func (f *educationMessages) ListChanges(ctx context.Context, q repository.MessageChangeQuery) ([]domain.Message, error) {
	var rows []domain.Message
	for _, m := range f.items {
		if m.LastEventSequence > q.AfterEventSequence {
			rows = append(rows, m)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LastEventSequence < rows[j].LastEventSequence })
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

type educationDialogStore struct{ fakeDialogs }

type educationReadStore struct {
	repository.MentorRepository
	values map[string]int64
}

func (r *educationReadStore) ReadSequence(_ context.Context, b, a, m uuid.UUID) (int64, error) {
	return r.values[b.String()+a.String()+m.String()], nil
}
func (r *educationReadStore) AdvanceRead(_ context.Context, b, a, m uuid.UUID, n int64) error {
	k := b.String() + a.String() + m.String()
	r.values[k] = max(r.values[k], n)
	return nil
}

func (f *educationDialogStore) PublicProjection(context.Context, uuid.UUID, uuid.UUID, int64) (repository.PublicDialogProjection, error) {
	return repository.PublicDialogProjection{}, nil
}
func educationMessageFixture() (Service, domain.Actor, *educationGuard, *educationMessages, *fakeOutbox, context.Context) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	actor := domain.Actor{UserID: uuid.New(), Role: "STUDENT"}
	mentor := uuid.New()
	d := domain.Dialog{ID: uuid.New(), SpaceID: uuid.New(), Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "Mentor cohort", CreatedBy: mentor, Version: 1, MemberCount: 1, CreatedAt: now, UpdatedAt: now}
	g := &educationGuard{access: domain.MentorAccess{Binding: domain.MentorBinding{ID: uuid.New(), DialogID: d.ID, Kind: domain.MentorCohort, SourceID: uuid.New(), MentorID: mentor}, MembershipID: uuid.New(), Revision: 7, HistoryFromRevision: 4}}
	g.authors = map[uuid.UUID]uuid.UUID{actor.UserID: g.access.MembershipID}
	messages := &educationMessages{fakeMessages: fakeMessages{items: map[uuid.UUID]domain.Message{}}}
	outbox := &fakeOutbox{}
	dialogs := &educationDialogStore{fakeDialogs{item: d}}
	service := Service{Education: g, Spaces: fakeSpaces{item: activeTestSpace(d.SpaceID, mentor, now)}, Dialogs: dialogs, Messages: messages, Members: &fakeMembers{items: map[uuid.UUID]domain.Member{mentor: activeMember(d.ID, mentor, 0, 0, now)}}, Outbox: outbox, Tx: fakeTx{}, Now: func() time.Time { return now }, NewID: uuid.New}
	return service, actor, g, messages, outbox, domain.WithMentorBinding(context.Background(), g.access.Binding.ID)
}
func TestMentorEducationCreateRetryRevokeAndOrdinaryIsolation(t *testing.T) {
	s, a, g, m, o, ctx := educationMessageFixture()
	in := CreateInput{DialogID: g.access.Binding.DialogID, IdempotencyKey: uuid.New(), Body: "educational content"}
	created, err := s.Create(ctx, a, in)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Created || created.View.Message.MentorBindingID == nil || created.View.Message.MentorAuthorMembershipID != g.access.MembershipID || len(o.items) != 0 || len(m.items) != 1 {
		t.Fatalf("wrong educational production: %+v", created)
	}
	replay, err := s.Create(ctx, a, in)
	if err != nil || replay.Created || replay.View.Message.ID != created.View.Message.ID || len(m.items) != 1 {
		t.Fatalf("retry=%+v %v", replay, err)
	}
	if _, err = s.Get(context.Background(), a, created.View.Message.ID); !errors.Is(err, domain.ErrDialogNotFound) {
		t.Fatalf("generic body leaked=%v", err)
	}
	g.denyAt = g.calls + 1
	if _, err = s.Create(ctx, a, in); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("stale replay=%v", err)
	}
	if _, err = s.Get(ctx, a, created.View.Message.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("revoked get=%v", err)
	}
}
func TestMentorEducationReadFiltersRejoinAndCurrentAuthors(t *testing.T) {
	s, a, g, m, _, ctx := educationMessageFixture()
	b := g.access.Binding.ID
	for n := int64(1); n <= 3; n++ {
		v := messageAt(g.access.Binding.DialogID, a.UserID, n, time.Now())
		v.MentorBindingID = &b
		v.MentorContextRevision = 7
		v.MentorAuthorMembershipID = g.access.MembershipID
		if n == 1 {
			v.MentorContextRevision = 2
		}
		if n == 2 {
			v.MentorAuthorMembershipID = uuid.New()
		}
		m.items[v.ID] = v
	}
	views, err := s.List(ctx, a, repository.MessageListQuery{DialogID: g.access.Binding.DialogID, Limit: 20})
	if err != nil || len(views) != 1 || views[0].Message.MessageSequence != 3 {
		t.Fatalf("history boundary=%+v %v", views, err)
	}
	g.authors = map[uuid.UUID]uuid.UUID{}
	views, err = s.List(ctx, a, repository.MessageListQuery{DialogID: g.access.Binding.DialogID, Limit: 20})
	if err != nil || len(views) != 0 {
		t.Fatalf("revoked author=%+v %v", views, err)
	}
}
func TestMentorEducationPaginationAdvancesPastRevokedAuthors(t *testing.T) {
	s, a, g, m, _, ctx := educationMessageFixture()
	b := g.access.Binding.ID
	denied := uuid.New()
	for n := int64(1); n <= 22; n++ {
		v := messageAt(g.access.Binding.DialogID, a.UserID, n, time.Now())
		v.MentorBindingID = &b
		v.MentorContextRevision = 7
		v.MentorAuthorMembershipID = g.access.MembershipID
		if n > 1 {
			v.SenderID = denied
		}
		m.items[v.ID] = v
	}
	first, err := s.MentorPage(ctx, a, repository.MessageListQuery{DialogID: g.access.Binding.DialogID, Limit: 20})
	if err != nil || len(first.Items) != 0 || first.Next == nil || first.Next.ID != b {
		t.Fatalf("first page=%+v %v", first, err)
	}
	second, err := s.MentorPage(ctx, a, repository.MessageListQuery{DialogID: g.access.Binding.DialogID, Limit: 20, Before: first.Next})
	if err != nil || len(second.Items) != 1 || second.Items[0].Message.MessageSequence != 1 {
		t.Fatalf("later current history starved=%+v %v", second, err)
	}
	for id, v := range m.items {
		v.LastEventSequence = 23 - v.MessageSequence
		m.items[id] = v
	}
	first, err = s.MentorChanges(ctx, a, repository.MessageChangeQuery{DialogID: g.access.Binding.DialogID, Limit: 20})
	if err != nil || len(first.Items) != 0 || first.NextAfter != 20 || !first.HasMore {
		t.Fatalf("changes=%+v %v", first, err)
	}
	second, err = s.MentorChanges(ctx, a, repository.MessageChangeQuery{DialogID: g.access.Binding.DialogID, AfterEventSequence: first.NextAfter, Limit: 20})
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("current change starved=%+v %v", second, err)
	}
}

func TestMentorEducationReadCursorRequiresVisibleMessageAndCurrentMembership(t *testing.T) {
	s, a, g, _, o, ctx := educationMessageFixture()
	reads := &educationReadStore{values: map[string]int64{}}
	s.MentorReads = reads
	message, err := s.Create(ctx, a, CreateInput{DialogID: g.access.Binding.DialogID, IdempotencyKey: uuid.New(), Body: "read me"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.ReadThrough(ctx, a, g.access.Binding.DialogID, message.View.Message.MessageSequence)
	if err != nil || state.LastReadMessageSequence != 1 || len(o.items) != 0 {
		t.Fatal(state, err)
	}
	if _, err = s.ReadThrough(ctx, a, g.access.Binding.DialogID, 50); !errors.Is(err, domain.ErrInvalidReadSequence) {
		t.Fatal("guessed hidden sequence accepted", err)
	}
	previous := g.access.MembershipID
	g.access.MembershipID = uuid.New()
	g.authors[a.UserID] = g.access.MembershipID
	if _, err = s.ReadThrough(ctx, a, g.access.Binding.DialogID, 1); !errors.Is(err, domain.ErrInvalidReadSequence) {
		t.Fatal("new enrollment read old message", err)
	}
	old, _ := reads.ReadSequence(ctx, g.access.Binding.ID, a.UserID, previous)
	fresh, _ := reads.ReadSequence(ctx, g.access.Binding.ID, a.UserID, g.access.MembershipID)
	if old != 1 || fresh != 0 {
		t.Fatal("read cursor crossed enrollment")
	}
}

func TestMentorEducationReplyReferenceRechecksCurrentParentAuthor(t *testing.T) {
	s, actor, guard, messages, _, ctx := educationMessageFixture()
	binding := guard.access.Binding.ID
	parentAuthor, parentMembership := uuid.New(), uuid.New()
	guard.authors[parentAuthor] = parentMembership
	makeMessage := func(author, membership uuid.UUID, sequence, revision int64) domain.Message {
		m := messageAt(guard.access.Binding.DialogID, author, sequence, time.Now())
		m.MentorBindingID = &binding
		m.MentorAuthorMembershipID = membership
		m.MentorContextRevision = revision
		return m
	}
	parent := makeMessage(parentAuthor, parentMembership, 2, 7)
	messages.items[parent.ID] = parent
	child := makeMessage(actor.UserID, guard.access.MembershipID, 3, 7)
	child.ReplyToMessageID = &parent.ID
	// A scanned row can introduce the parent author into the bounded proof
	// without itself remaining visible in the result's history interval.
	old := makeMessage(parentAuthor, parentMembership, 1, 2)
	before := guard.access
	before.ActorID = actor.UserID
	ctx = domain.WithMentorAccess(ctx, &before)
	guard.onCheck = func(call int) {
		if call == 2 {
			delete(guard.authors, parentAuthor)
		}
	}
	views, err := s.mentorViews(ctx, actor.UserID, guard.access.Binding.DialogID, []View{{Message: old}, {Message: child}})
	if err != nil || len(views) != 1 {
		t.Fatalf("current child must remain visible: %+v, %v", views, err)
	}
	if views[0].Message.ReplyToMessageID != nil {
		t.Fatal("reply reference exposed after parent author lost current access")
	}
}

func TestMentorEducationMutationResponsesPreserveSanitizedReply(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			s, actor, guard, messages, _, ctx := educationMessageFixture()
			binding := guard.access.Binding.ID
			parent := messageAt(guard.access.Binding.DialogID, uuid.New(), 1, s.now())
			parent.MentorBindingID, parent.MentorContextRevision, parent.MentorAuthorMembershipID = &binding, 7, uuid.New()
			// This parent's old enrollment is no longer in current owner authors.
			messages.items[parent.ID] = parent
			child := messageAt(guard.access.Binding.DialogID, actor.UserID, 2, s.now())
			child.MentorBindingID, child.MentorContextRevision, child.MentorAuthorMembershipID = &binding, 7, guard.access.MembershipID
			child.ReplyToMessageID = &parent.ID
			messages.items[child.ID] = child
			var view View
			var err error
			if operation == "update" {
				view, err = s.Update(ctx, actor, UpdateInput{MessageID: child.ID, ExpectedVersion: child.Version, Body: "edited current message"})
			} else {
				view, err = s.Delete(ctx, actor, DeleteInput{MessageID: child.ID, ExpectedVersion: child.Version})
			}
			if err != nil || view.Message.ID != child.ID || view.Message.Version != child.Version+1 {
				t.Fatalf("mutation=%+v, %v", view, err)
			}
			if view.Message.ReplyToMessageID != nil {
				t.Fatal("mutation response discarded the sanitized reply projection")
			}
			if messages.items[parent.ID].Body != parent.Body {
				t.Fatal("access reduction must preserve stored history")
			}
		})
	}
}
