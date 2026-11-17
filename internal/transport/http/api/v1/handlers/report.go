package handlers

import (
	"github.com/bemulima/ms-go-dialog/internal/domain"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common"
	"github.com/bemulima/ms-go-dialog/internal/transport/http/common/middleware"
	reportuc "github.com/bemulima/ms-go-dialog/internal/usecase/report"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"strconv"
)

type ReportHandler struct{ Service *reportuc.Service }

func reportKey(r *http.Request, id uuid.UUID) bool {
	return id != uuid.Nil && len(r.Header.Values("Idempotency-Key")) == 1 && r.Header.Get("Idempotency-Key") == id.String()
}
func (h ReportHandler) Create(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	var in reportuc.CreateInput
	if r.URL.RawQuery != "" || r.URL.ForceQuery || common.DecodeExactJSON(w, r, &in, 4096, "request_id", "message_id", "category", "details") != nil || !reportKey(r, in.RequestID) {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.Create(r.Context(), middleware.Actor(r), in)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, 200, result)
}
func (h ReportHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	q, err := commonReportQuery(r)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	var before *uuid.UUID
	if raw := q["before_id"]; raw != "" {
		id, e := uuid.Parse(raw)
		if e != nil || id == uuid.Nil || id.String() != raw {
			common.WriteError(w, r, domain.ErrValidation)
			return
		}
		before = &id
	}
	limit := 20
	if raw := q["limit"]; raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || strconv.Itoa(limit) != raw {
			common.WriteError(w, r, domain.ErrValidation)
			return
		}
	}
	result, err := h.Service.List(r.Context(), middleware.Actor(r), before, limit)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, 200, result)
}
func commonReportQuery(r *http.Request) (map[string]string, error) {
	q := r.URL.Query()
	out := map[string]string{}
	for key, values := range q {
		if (key != "limit" && key != "before_id") || len(values) != 1 || values[0] == "" {
			return nil, domain.ErrValidation
		}
		out[key] = values[0]
	}
	return out, nil
}
func (h ReportHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, err := uuid.Parse(chi.URLParam(r, "reportID"))
	var in reportuc.ResolveInput
	if err != nil || id == uuid.Nil || id.String() != chi.URLParam(r, "reportID") || r.URL.RawQuery != "" || r.URL.ForceQuery || common.DecodeExactJSON(w, r, &in, 4096, "request_id", "expected_revision", "action", "reason") != nil || !reportKey(r, in.RequestID) {
		common.WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.Resolve(r.Context(), middleware.Actor(r), id, in)
	if err != nil {
		common.WriteError(w, r, err)
		return
	}
	common.WriteJSON(w, 200, map[string]any{"schema": "dialog.report-resolution.v1", "report": result})
}
