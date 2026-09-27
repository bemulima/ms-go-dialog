package attachment

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/domain/repository"
	"github.com/google/uuid"
)

type workerTx struct{}

func (workerTx) WithinTransaction(ctx context.Context, run func(context.Context) error) error {
	return run(ctx)
}

type workerFiles struct{ trace *[]string }

func (f workerFiles) UploadTemporary(context.Context, TemporaryFileInput) (StoredFile, error) {
	return StoredFile{}, nil
}
func (f workerFiles) Activate(context.Context, uuid.UUID) error {
	*f.trace = append(*f.trace, "filestorage_activate")
	return nil
}
func (workerFiles) SignedGETURL(context.Context, uuid.UUID, int) (string, error) { return "", nil }
func (f workerFiles) Delete(context.Context, uuid.UUID) error {
	*f.trace = append(*f.trace, "filestorage_delete")
	return nil
}

type workerAttachments struct {
	repository.AttachmentRepository
	trace                  *[]string
	source                 domain.Attachment
	activationClaimed      bool
	claimNow, claimExpires time.Time
}

func (f *workerAttachments) ListExpired(context.Context, time.Time, int) ([]domain.Attachment, error) {
	*f.trace = append(*f.trace, "list_expired")
	return nil, nil
}
func (f *workerAttachments) ClaimForActivation(_ context.Context, now, leaseUntil time.Time, _ int) ([]domain.Attachment, error) {
	*f.trace = append(*f.trace, "claim_activation")
	f.claimNow, f.claimExpires = now, leaseUntil
	if f.activationClaimed {
		return nil, nil
	}
	f.activationClaimed = true
	return []domain.Attachment{f.source}, nil
}
func (f *workerAttachments) MarkReadyIfProcessing(_ context.Context, id uuid.UUID, now time.Time) (domain.Attachment, bool, error) {
	*f.trace = append(*f.trace, "attachment_transition")
	if id != f.source.ID {
		return domain.Attachment{}, false, domain.ErrNotFound
	}
	item := f.source
	item.Status = domain.AttachmentStatusReady
	item.ActivatedAt = &now
	item.UpdatedAt = now
	f.source = item
	return item, true, nil
}
func (f *workerAttachments) ListByMessage(context.Context, uuid.UUID, uuid.UUID) ([]domain.Attachment, error) {
	*f.trace = append(*f.trace, "list_message_attachments")
	return []domain.Attachment{f.source}, nil
}
func (f *workerAttachments) ClaimForDeletion(context.Context, time.Time, time.Time, int) ([]domain.Attachment, error) {
	*f.trace = append(*f.trace, "claim_deletion")
	return nil, nil
}

type workerMessages struct {
	repository.MessageRepository
	trace   *[]string
	message domain.Message
}

func (f *workerMessages) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Message, error) {
	*f.trace = append(*f.trace, "message_lock")
	return f.message, nil
}
func (f *workerMessages) AdvanceEvent(_ context.Context, id uuid.UUID, sequence int64) (domain.Message, error) {
	*f.trace = append(*f.trace, "message_advance")
	if id != f.message.ID {
		return domain.Message{}, domain.ErrNotFound
	}
	f.message.LastEventSequence = sequence
	f.message.Version++
	return f.message, nil
}

type workerDialogs struct {
	repository.DialogRepository
	trace  *[]string
	dialog domain.Dialog
}

func (f *workerDialogs) GetByIDForUpdate(context.Context, uuid.UUID) (domain.Dialog, error) {
	*f.trace = append(*f.trace, "dialog_lock")
	return f.dialog, nil
}
func (f *workerDialogs) UpdateState(_ context.Context, item domain.Dialog, _ int) error {
	*f.trace = append(*f.trace, "dialog_update")
	f.dialog = item
	return nil
}

type workerOutbox struct {
	repository.OutboxRepository
	trace *[]string
	items []domain.OutboxEvent
}

func (f *workerOutbox) Add(_ context.Context, item domain.OutboxEvent) error {
	*f.trace = append(*f.trace, "outbox_add")
	f.items = append(f.items, item)
	return nil
}

func TestProcess_ClaimsWorkAndUsesAPILockOrder(t *testing.T) {
	now := time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)
	dialogID, messageID, attachmentID := uuid.New(), uuid.New(), uuid.New()
	trace := make([]string, 0)
	source := domain.Attachment{
		ID: attachmentID, DialogID: dialogID, MessageID: &messageID, UploaderID: uuid.New(), FileStorageID: uuid.New(),
		Kind: domain.AttachmentKindFile, Status: domain.AttachmentStatusProcessing, MIMEType: "application/pdf", SizeBytes: 10,
		OriginalFilename: "file.pdf", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}
	attachments := &workerAttachments{trace: &trace, source: source}
	messages := &workerMessages{trace: &trace, message: domain.Message{
		ID: messageID, DialogID: dialogID, SenderID: uuid.New(), Body: "body", Status: domain.MessageStatusActive,
		Version: 1, MessageSequence: 1, LastEventSequence: 1, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now,
	}}
	dialogs := &workerDialogs{trace: &trace, dialog: domain.Dialog{
		ID: dialogID, SpaceID: uuid.New(), Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive,
		Title: "group", CreatedBy: uuid.New(), Version: 1, MemberCount: 2, MessageCount: 1,
		MaxMessageSequence: 1, MaxEventSequence: 1, LastMessageID: &messageID, LastMessageAt: &now, CreatedAt: now, UpdatedAt: now,
	}}
	outbox := &workerOutbox{trace: &trace}
	service := Service{
		Dialogs: dialogs, Messages: messages, Attachments: attachments, Outbox: outbox, Tx: workerTx{},
		Files: workerFiles{trace: &trace}, Now: func() time.Time { return now }, NewID: uuid.New, WorkerLease: 2 * time.Minute,
	}

	result, err := service.Process(context.Background(), 10)
	if err != nil || result.Activated != 1 {
		t.Fatalf("process result=%+v err=%v trace=%v", result, err, trace)
	}
	if attachments.claimExpires.Sub(attachments.claimNow) != 2*time.Minute {
		t.Fatalf("worker lease=%s", attachments.claimExpires.Sub(attachments.claimNow))
	}
	messageLock := slices.Index(trace, "message_lock")
	dialogLock := slices.Index(trace, "dialog_lock")
	transition := slices.Index(trace, "attachment_transition")
	if messageLock < 0 || dialogLock <= messageLock || transition <= dialogLock {
		t.Fatalf("unsafe lifecycle lock order: %v", trace)
	}
	if len(outbox.items) != 1 || outbox.items[0].Subject != domain.EventDialogAttachmentReady {
		t.Fatalf("attachment event mismatch: %+v", outbox.items)
	}
}

func TestProcess_HiddenMessageCompletesAttachmentWithoutPublicEvent(t *testing.T) {
	now := time.Now().UTC()
	dialogID, messageID := uuid.New(), uuid.New()
	trace := make([]string, 0)
	source := domain.Attachment{ID: uuid.New(), DialogID: dialogID, MessageID: &messageID, UploaderID: uuid.New(), FileStorageID: uuid.New(), Kind: domain.AttachmentKindFile, Status: domain.AttachmentStatusProcessing, MIMEType: "application/pdf", SizeBytes: 10, OriginalFilename: "file.pdf", ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
	attachments := &workerAttachments{trace: &trace, source: source}
	messages := &workerMessages{trace: &trace, message: domain.Message{ID: messageID, DialogID: dialogID, SenderID: uuid.New(), Body: "body", Status: domain.MessageStatusHidden, Version: 2, MessageSequence: 1, LastEventSequence: 2, IdempotencyKey: uuid.New(), CreatedAt: now, UpdatedAt: now}}
	dialogs := &workerDialogs{trace: &trace, dialog: domain.Dialog{ID: dialogID, SpaceID: uuid.New(), Type: domain.DialogTypeGroup, Status: domain.DialogStatusActive, Title: "group", CreatedBy: uuid.New(), Version: 2, MemberCount: 2, MessageCount: 1, MaxMessageSequence: 1, MaxEventSequence: 2, LastMessageID: &messageID, LastMessageAt: &now, CreatedAt: now, UpdatedAt: now}}
	outbox := &workerOutbox{trace: &trace}
	service := Service{Dialogs: dialogs, Messages: messages, Attachments: attachments, Outbox: outbox, Tx: workerTx{}, Files: workerFiles{trace: &trace}, Now: func() time.Time { return now }}

	result, err := service.Process(context.Background(), 1)
	if err != nil || result.Activated != 1 {
		t.Fatalf("process result=%+v err=%v trace=%v", result, err, trace)
	}
	if len(outbox.items) != 0 || slices.Contains(trace, "message_advance") || slices.Contains(trace, "dialog_update") {
		t.Fatalf("hidden message emitted lifecycle evidence: trace=%v events=%d", trace, len(outbox.items))
	}
}

type uploadContractSpaceRepository struct {
	repository.SpaceRepository
	space domain.Space
}

func (r uploadContractSpaceRepository) GetByID(_ context.Context, id uuid.UUID) (domain.Space, error) {
	if id != r.space.ID {
		return domain.Space{}, domain.ErrNotFound
	}
	return r.space, nil
}

type uploadContractDialogRepository struct {
	repository.DialogRepository
	dialog domain.Dialog
}

func (r uploadContractDialogRepository) GetByID(_ context.Context, id uuid.UUID) (domain.Dialog, error) {
	if id != r.dialog.ID {
		return domain.Dialog{}, domain.ErrNotFound
	}
	return r.dialog, nil
}

type uploadContractMemberRepository struct {
	repository.MemberRepository
	member domain.Member
}

func (r uploadContractMemberRepository) Get(_ context.Context, dialogID, userID uuid.UUID) (domain.Member, error) {
	if dialogID != r.member.DialogID || userID != r.member.UserID {
		return domain.Member{}, domain.ErrNotFound
	}
	return r.member, nil
}

type uploadContractAttachmentRepository struct {
	repository.AttachmentRepository
	pendingCalls int
	createCalls  int
}

func (r *uploadContractAttachmentRepository) CountPendingByUploader(context.Context, uuid.UUID, uuid.UUID, time.Time) (int, error) {
	r.pendingCalls++
	return 0, nil
}

func (r *uploadContractAttachmentRepository) Create(context.Context, domain.Attachment) error {
	r.createCalls++
	return nil
}

type uploadContractFileStorage struct{ uploadCalls int }

func (f *uploadContractFileStorage) UploadTemporary(context.Context, TemporaryFileInput) (StoredFile, error) {
	f.uploadCalls++
	return StoredFile{ID: uuid.New()}, nil
}
func (*uploadContractFileStorage) Activate(context.Context, uuid.UUID) error { return nil }
func (*uploadContractFileStorage) SignedGETURL(context.Context, uuid.UUID, int) (string, error) {
	return "https://filestorage.test/signed-object", nil
}
func (*uploadContractFileStorage) Delete(context.Context, uuid.UUID) error { return nil }

type uploadContractScanner struct {
	err   error
	calls int
}

func (s *uploadContractScanner) Scan(context.Context, []byte) error {
	s.calls++
	return s.err
}

func TestUpload_FailsClosedBeforeFileStorageWhenClamAVRejects(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "scanner unavailable", err: domain.ErrFileScanUnavailable},
		{name: "infected content", err: domain.ErrFileInfected},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			spaceID, dialogID, userID := uuid.New(), uuid.New(), uuid.New()
			files := &uploadContractFileStorage{}
			scanner := &uploadContractScanner{err: test.err}
			attachments := &uploadContractAttachmentRepository{}
			service := Service{
				Spaces:      uploadContractSpaceRepository{space: domain.Space{ID: spaceID, Status: domain.SpaceStatusActive, Policy: domain.DefaultPolicy()}},
				Dialogs:     uploadContractDialogRepository{dialog: domain.Dialog{ID: dialogID, SpaceID: spaceID, Status: domain.DialogStatusActive}},
				Members:     uploadContractMemberRepository{member: domain.Member{DialogID: dialogID, UserID: userID, Status: domain.MemberStatusActive}},
				Attachments: attachments, Files: files, Scanner: scanner, Now: func() time.Time { return now },
			}

			_, err := service.Upload(context.Background(), domain.Actor{UserID: userID, Role: "USER"}, UploadInput{
				DialogID: dialogID, Filename: "notes.txt", Data: []byte("ordinary text file"),
			})
			if !errors.Is(err, test.err) {
				t.Fatalf("upload error=%v, want %v", err, test.err)
			}
			if scanner.calls != 1 || files.uploadCalls != 0 || attachments.pendingCalls != 0 || attachments.createCalls != 0 {
				t.Fatalf("fail-closed side effects: scans=%d file_uploads=%d pending_checks=%d metadata_creates=%d", scanner.calls, files.uploadCalls, attachments.pendingCalls, attachments.createCalls)
			}
		})
	}
}
