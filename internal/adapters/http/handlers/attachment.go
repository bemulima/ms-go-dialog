package handlers

import (
	"io"
	"net/http"

	"github.com/bemulima/ms-go-dialog/internal/adapters/http/middleware"
	"github.com/bemulima/ms-go-dialog/internal/domain"
	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxMultipartBodyBytes = domain.HardMaxFileBytes + (1 << 20)

type AttachmentHandler struct{ Service *attachmentuc.Service }

func (h AttachmentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		WriteError(w, r, domain.ErrInvalidAttachment)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	dialogID, err := uuid.Parse(r.FormValue("dialog_id"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		WriteError(w, r, domain.ErrInvalidAttachment)
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size < 1 || header.Size > domain.HardMaxFileBytes {
		WriteError(w, r, domain.ErrInvalidAttachment)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, domain.HardMaxFileBytes+1))
	if err != nil || int64(len(data)) > domain.HardMaxFileBytes {
		WriteError(w, r, domain.ErrInvalidAttachment)
		return
	}
	item, err := h.Service.Upload(r.Context(), middleware.Actor(r), attachmentuc.UploadInput{DialogID: dialogID, Filename: header.Filename, Data: data})
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newAttachmentResponse(item))
}
func (h AttachmentHandler) SignedURL(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	result, err := h.Service.GetSignedURL(r.Context(), middleware.Actor(r), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h AttachmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "attachmentID"))
	if err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	if err := decodeOptionalEmptyBody(r); err != nil {
		WriteError(w, r, domain.ErrValidation)
		return
	}
	item, err := h.Service.Delete(r.Context(), middleware.Actor(r), id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newAttachmentResponse(item))
}
