package handlers

import (
	"github.com/bemulima/ms-go-dialog/internal/domain"
	common "github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	realtimeuc "github.com/bemulima/ms-go-dialog/internal/usecase/realtime"
	"github.com/google/uuid"
	"net/http"
)

type RealtimeHandler struct{ Service *realtimeuc.TicketService }

func (h RealtimeHandler) MintTicket(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SpaceID uuid.UUID `json:"space_id"`
	}
	if err := common.DecodeJSON(w, r, &request); err != nil {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.Mint(r.Context(), middleware.Actor(r), realtimeuc.MintTicketInput{SpaceID: request.SpaceID})
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, result)
}
