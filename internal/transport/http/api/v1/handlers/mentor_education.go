package handlers

import (
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
	"net/http"
)

type MentorEducationHandler struct {
	Service *dialoguc.MentorEducationService
}

func (h MentorEducationHandler) Ensure(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Content-Encoding")) > 0 || domain.SelectedMentorBinding(r.Context()) != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	var wire struct {
		Kind domain.MentorContextKind `json:"kind"`
		ID   string                   `json:"id"`
	}
	if common.DecodeExactJSON(w, r, &wire, 1024, "kind", "id") != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	id, parseErr := uuid.Parse(wire.ID)
	if parseErr != nil || id == uuid.Nil || id.String() != wire.ID {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	in := dialoguc.MentorEnsureInput{Kind: wire.Kind, ID: id}
	result, err := h.Service.Ensure(r.Context(), middleware.Actor(r), in)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, 200, result)
}
