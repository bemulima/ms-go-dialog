package attachment

import (
	"context"
	"errors"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
	"testing"
	"time"
)

type mentorAttachmentGuard struct {
	access domain.MentorAccess
	denied bool
}

func (g *mentorAttachmentGuard) Protected(context.Context, uuid.UUID) (bool, error) { return true, nil }
func (g *mentorAttachmentGuard) Check(ctx context.Context, user, id uuid.UUID, _ []uuid.UUID) (*domain.MentorAccess, error) {
	if g.denied {
		return nil, domain.ErrForbidden
	}
	if domain.SelectedMentorBinding(ctx) == nil {
		return nil, domain.ErrDialogNotFound
	}
	v := g.access
	v.ActorID = user
	return &v, nil
}

type mentorAttachmentStore struct {
	repository.AttachmentRepository
	item domain.Attachment
}

func (r *mentorAttachmentStore) GetByID(context.Context, uuid.UUID) (domain.Attachment, error) {
	return r.item, nil
}

type mentorAttachmentMessages struct {
	repository.MessageRepository
	item domain.Message
}

func (r *mentorAttachmentMessages) GetByID(context.Context, uuid.UUID) (domain.Message, error) {
	return r.item, nil
}

type mentorSigningFiles struct {
	FileStorage
	calls int
	after func()
}

func (f *mentorSigningFiles) SignedGETURL(context.Context, uuid.UUID, int) (string, error) {
	f.calls++
	if f.after != nil {
		f.after()
	}
	return "https://files.invalid/signed-private", nil
}
func TestMentorEducationAttachmentRechecksAfterSigning(t *testing.T) {
	now := time.Now().UTC()
	actor := domain.Actor{UserID: uuid.New(), Role: "STUDENT"}
	binding, dialog, space, mentor, membership, msg := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	g := &mentorAttachmentGuard{access: domain.MentorAccess{Binding: domain.MentorBinding{ID: binding, DialogID: dialog, Kind: domain.MentorCohort, MentorID: mentor}, MembershipID: membership, Revision: 7, HistoryFromRevision: 3, Authors: map[uuid.UUID]uuid.UUID{mentor: membership}}}
	files := &mentorSigningFiles{}
	attachment := domain.Attachment{ID: uuid.New(), DialogID: dialog, MessageID: &msg, Status: domain.AttachmentStatusReady, MentorBindingID: &binding, FileStorageID: uuid.New()}
	s := Service{Education: g, Attachments: &mentorAttachmentStore{item: attachment}, Messages: &mentorAttachmentMessages{item: domain.Message{ID: msg, DialogID: dialog, SenderID: mentor, Status: domain.MessageStatusActive, MessageSequence: 5, MentorBindingID: &binding, MentorContextRevision: 7, MentorAuthorMembershipID: membership}}, Dialogs: uploadContractDialogRepository{dialog: domain.Dialog{ID: dialog, SpaceID: space, Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, CreatedAt: now}}, Spaces: uploadContractSpaceRepository{space: domain.Space{ID: space, Status: domain.SpaceStatusActive}}, Files: files}
	ctx := domain.WithMentorBinding(context.Background(), binding)
	got, err := s.GetSignedURL(ctx, actor, attachment.ID)
	if err != nil || got.URL == "" {
		t.Fatal(got, err)
	}
	files.after = func() { g.denied = true }
	got, err = s.GetSignedURL(ctx, actor, attachment.ID)
	if !errors.Is(err, domain.ErrForbidden) || got.URL != "" {
		t.Fatalf("revoke leaked URL=%+v %v", got, err)
	}
	before := files.calls
	g.denied = false
	if _, err = s.GetSignedURL(context.Background(), actor, attachment.ID); err == nil || files.calls != before {
		t.Fatal("generic URL leaked")
	}
}

func TestMentorEducationAttachmentReadyAdvancesChangesWithoutGenericEvent(t *testing.T) {
	now := time.Now().UTC()
	trace := []string{}
	dialogID, messageID, bindingID := uuid.New(), uuid.New(), uuid.New()
	attachments := &workerAttachments{trace: &trace, source: domain.Attachment{
		ID: uuid.New(), DialogID: dialogID, MessageID: &messageID, MentorBindingID: &bindingID,
		FileStorageID: uuid.New(), Status: domain.AttachmentStatusProcessing, ExpiresAt: now.Add(time.Hour),
	}}
	messages := &workerMessages{trace: &trace, message: domain.Message{
		ID: messageID, DialogID: dialogID, MentorBindingID: &bindingID, SenderID: uuid.New(),
		Status: domain.MessageStatusActive, Version: 2, MessageSequence: 1, LastEventSequence: 2,
	}}
	dialogs := &workerDialogs{trace: &trace, dialog: domain.Dialog{ID: dialogID, Version: 2, MaxEventSequence: 2}}
	outbox := &workerOutbox{trace: &trace}
	s := Service{Attachments: attachments, Messages: messages, Dialogs: dialogs, Outbox: outbox,
		Files: workerFiles{trace: &trace}, Tx: workerTx{}, Now: func() time.Time { return now }}
	result, err := s.Process(context.Background(), 1)
	if err != nil || result.Activated != 1 || attachments.source.Status != domain.AttachmentStatusReady {
		t.Fatalf("activation=%+v, %v", result, err)
	}
	if messages.message.LastEventSequence != 3 || messages.message.Version != 3 || dialogs.dialog.MaxEventSequence != 3 || dialogs.dialog.Version != 3 {
		t.Fatal("educational attachment readiness did not advance durable changes cursor")
	}
	if len(outbox.items) != 0 {
		t.Fatal("educational attachment metadata entered generic realtime outbox")
	}
}
