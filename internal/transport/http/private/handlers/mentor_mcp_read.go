package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

const MentorMCPDialogsPath = "/internal/v1/mentor-mcp/dialogs"
const MentorMCPMessagesPath = "/internal/v1/mentor-mcp/dialog-messages"
const mentorMCPReadHeader = "X-Mentor-Dialog-MCP-Read-Token"

type MentorMCPContexts interface {
	messageuc.MentorMCPBindingVerifier
	MentorMCPDialogs(context.Context, uuid.UUID, uuid.UUID, int) ([]dialoguc.MentorMCPDialog, uuid.UUID, error)
}

type MentorMCPMessages interface {
	MentorMCPMessages(context.Context, messageuc.MentorMCPBindingVerifier, uuid.UUID, uuid.UUID, uuid.UUID, int64, int) (messageuc.MentorMCPMessagePage, error)
}

type MentorMCPReadHandler struct {
	Token    string
	Limiter  middleware.ActorLimiter
	Contexts MentorMCPContexts
	Messages MentorMCPMessages
}

type mentorMCPDialogsInput struct {
	MentorID uuid.UUID `json:"mentor_id"`
	Cursor   string    `json:"cursor"`
	Limit    int       `json:"limit"`
}

type mentorMCPMessagesInput struct {
	MentorID       uuid.UUID `json:"mentor_id"`
	DialogID       uuid.UUID `json:"dialog_id"`
	BindingID      uuid.UUID `json:"binding_id"`
	BeforeSequence string    `json:"before_sequence"`
	Limit          int       `json:"limit"`
}

func (h MentorMCPReadHandler) ListDialogs(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, MentorMCPDialogsPath) {
		return
	}
	var input mentorMCPDialogsInput
	if common.DecodeExactJSON(w, r, &input, 1024, "mentor_id", "cursor", "limit") != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	after, err := h.openCursor(input.MentorID, input.Cursor)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	if !h.validPage(w, r, input.MentorID, input.Limit) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	items, next, err := h.Contexts.MentorMCPDialogs(ctx, input.MentorID, after, input.Limit)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	cursor, err := h.sealCursor(input.MentorID, next)
	if err != nil || ctx.Err() != nil || items == nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	common.WriteJSON(w, http.StatusOK, struct {
		Schema     string                     `json:"schema"`
		MentorID   uuid.UUID                  `json:"mentor_id"`
		Items      []dialoguc.MentorMCPDialog `json:"items"`
		NextCursor string                     `json:"next_cursor,omitempty"`
	}{"dialog.mentor-mcp-dialogs.v1", input.MentorID, items, cursor})
}

func (h MentorMCPReadHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, MentorMCPMessagesPath) {
		return
	}
	var input mentorMCPMessagesInput
	if common.DecodeExactJSON(w, r, &input, 1024, "mentor_id", "dialog_id", "binding_id", "before_sequence", "limit") != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var before int64
	if input.BeforeSequence != "" {
		n, err := strconv.ParseInt(input.BeforeSequence, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != input.BeforeSequence {
			common.WriteError(w, r, domain.ErrValidation)
			return
		}
		before = n
	}
	if !h.validPage(w, r, input.MentorID, input.Limit) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	page, err := h.Messages.MentorMCPMessages(ctx, h.Contexts, input.MentorID, input.DialogID, input.BindingID, before, input.Limit)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	if ctx.Err() != nil || page.Items == nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	common.WriteJSON(w, http.StatusOK, struct {
		Schema    string    `json:"schema"`
		MentorID  uuid.UUID `json:"mentor_id"`
		DialogID  uuid.UUID `json:"dialog_id"`
		BindingID uuid.UUID `json:"binding_id"`
		messageuc.MentorMCPMessagePage
	}{"dialog.mentor-mcp-messages.v1", input.MentorID, input.DialogID, input.BindingID, page})
}

func (h MentorMCPReadHandler) authorize(w http.ResponseWriter, r *http.Request, path string) bool {
	w.Header().Set("Cache-Control", "no-store")
	tokens := r.Header.Values(mentorMCPReadHeader)
	if h.Token == "" || len(tokens) != 1 || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(h.Token)) != 1 {
		common.WriteError(w, r, domain.ErrAuthentication)
		return false
	}
	for _, header := range []string{"Authorization", "Cookie", "X-User-ID", "X-User-Role", "X-Internal-Token", "X-Mentor-Dialog-Context"} {
		if len(r.Header.Values(header)) != 0 {
			common.WriteError(w, r, domain.ErrAuthentication)
			return false
		}
	}
	if r.Method != http.MethodPost || r.URL.EscapedPath() != path || r.URL.RawQuery != "" || r.URL.ForceQuery {
		common.WriteError(w, r, domain.ErrValidation)
		return false
	}
	return true
}

func (h MentorMCPReadHandler) validPage(w http.ResponseWriter, r *http.Request, mentor uuid.UUID, limit int) bool {
	if limit < 1 || limit > dialoguc.MentorMCPPageLimit {
		common.WriteError(w, r, domain.ErrValidation)
		return false
	}
	if h.Contexts == nil || h.Messages == nil || h.Limiter == nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return false
	}
	if !h.Limiter.Allow(mentor) {
		common.WriteError(w, r, domain.ErrRateLimited)
		return false
	}
	return true
}

func (h MentorMCPReadHandler) cursorCipher() (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("dialog.mentor-mcp-binding-cursor.v1\n" + h.Token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (h MentorMCPReadHandler) openCursor(mentor uuid.UUID, input string) (uuid.UUID, error) {
	if input == "" {
		return uuid.Nil, nil
	}
	if len(input) != 59 {
		return uuid.Nil, domain.ErrValidation
	}
	opened, err := base64.RawURLEncoding.Strict().DecodeString(input)
	if err != nil {
		return uuid.Nil, domain.ErrValidation
	}
	c, err := h.cursorCipher()
	if err != nil {
		return uuid.Nil, domain.ErrDependencyUnavailable
	}
	n := c.NonceSize()
	if len(opened) != n+len(uuid.Nil)+c.Overhead() {
		return uuid.Nil, domain.ErrValidation
	}
	plain, err := c.Open(nil, opened[:n], opened[n:], mentor[:])
	if err != nil {
		return uuid.Nil, domain.ErrValidation
	}
	id, err := uuid.FromBytes(plain)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, domain.ErrValidation
	}
	return id, nil
}

func (h MentorMCPReadHandler) sealCursor(mentor, after uuid.UUID) (string, error) {
	if after == uuid.Nil {
		return "", nil
	}
	c, err := h.cursorCipher()
	if err != nil {
		return "", domain.ErrDependencyUnavailable
	}
	nonce := make([]byte, c.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", domain.ErrDependencyUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(c.Seal(nonce, nonce, after[:], mentor[:])), nil
}
