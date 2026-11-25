package handlers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	messageuc "github.com/bemulima/ms-go-dialog/internal/usecase/message"
	"github.com/google/uuid"
)

const MentorMCPPreviewSendPath = "/internal/v1/mentor-mcp/message-preview"
const MentorMCPSendPath = "/internal/v1/mentor-mcp/message-send"

type MentorMCPSender interface {
	PreviewMentorMCPSend(context.Context, messageuc.MentorMCPSendInput) error
	SendMentorMCP(context.Context, messageuc.MentorMCPSendCommand) (messageuc.MentorMCPSendResult, error)
}

type MentorMCPSendHandler struct {
	Token    string
	Limiter  middleware.ActorLimiter
	Messages MentorMCPSender
}

var mentorMCPSendFields = []string{"mentor_id", "request_id", "kind", "source_id", "dialog_id", "binding_id", "expected_revision", "body"}

func (h MentorMCPSendHandler) Preview(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, MentorMCPPreviewSendPath) {
		return
	}
	var in messageuc.MentorMCPSendInput
	if common.DecodeExactJSON(w, r, &in, 32<<10, mentorMCPSendFields...) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if !h.rateLimit(w, r, in.MentorID) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.Messages.PreviewMentorMCPSend(ctx, in); err != nil {
		common.WriteError(w, r, err)
		return
	}
	if ctx.Err() != nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h MentorMCPSendHandler) Send(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, MentorMCPSendPath) {
		return
	}
	var command messageuc.MentorMCPSendCommand
	if common.DecodeExactJSON(w, r, &command, 32<<10, append(mentorMCPSendFields, "confirmation_id")...) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	if !h.rateLimit(w, r, command.MentorID) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out, err := h.Messages.SendMentorMCP(ctx, command)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	if ctx.Err() != nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (h MentorMCPSendHandler) authorize(w http.ResponseWriter, r *http.Request, path string) bool {
	w.Header().Set("Cache-Control", "no-store")
	tokens := r.Header.Values("X-Mentor-Dialog-MCP-Write-Token")
	if h.Token == "" || len(tokens) != 1 || subtle.ConstantTimeCompare([]byte(tokens[0]), []byte(h.Token)) != 1 {
		common.WriteError(w, r, domain.ErrAuthentication)
		return false
	}
	for _, header := range []string{"Authorization", "Cookie", "X-User-ID", "X-User-Role", "X-Internal-Token", "X-Mentor-Dialog-Context", "X-Mentor-Dialog-MCP-Read-Token", "Idempotency-Key"} {
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

func (h MentorMCPSendHandler) rateLimit(w http.ResponseWriter, r *http.Request, mentor uuid.UUID) bool {
	if h.Messages == nil || h.Limiter == nil {
		common.WriteError(w, r, domain.ErrDependencyUnavailable)
		return false
	}
	if !h.Limiter.Allow(mentor) {
		common.WriteError(w, r, domain.ErrRateLimited)
		return false
	}
	return true
}
