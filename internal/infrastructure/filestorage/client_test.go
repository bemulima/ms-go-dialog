package filestorage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	"github.com/google/uuid"
)

func TestClientUploadTemporaryStreamsExpectedContract(t *testing.T) {
	ownerID, storedID := uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/files/upload" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			http.Error(w, "invalid multipart", http.StatusBadRequest)
			return
		}
		for name, expected := range map[string]string{
			"file_kind": "USER_MEDIA", "owner_id": ownerID.String(), "is_temp": "true", "ttl_minutes": "60",
		} {
			if actual := r.FormValue(name); actual != expected {
				t.Errorf("%s=%q, want %q", name, actual, expected)
			}
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("form file: %v", err)
			http.Error(w, "file missing", http.StatusBadRequest)
			return
		}
		defer func() { _ = file.Close() }()
		payload, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("read file: %v", err)
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		if header.Filename != "photo.png" || string(payload) != "image bytes" {
			t.Errorf("unexpected file: name=%q payload=%q", header.Filename, payload)
			http.Error(w, "unexpected file", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"` + storedID.String() + `"}`))
	}))
	defer server.Close()

	result, err := (Client{BaseURL: server.URL, InternalToken: "test-token", HTTPClient: server.Client()}).UploadTemporary(context.Background(), attachmentuc.TemporaryFileInput{
		OwnerID: ownerID, Filename: "photo.png", Data: []byte("image bytes"), TTLMinutes: 60,
	})
	if err != nil || result.ID != storedID {
		t.Fatalf("upload result=%+v err=%v", result, err)
	}
}

func TestClientDeleteTreatsMissingFileAsIdempotent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	if err := (Client{BaseURL: server.URL, HTTPClient: server.Client()}).Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
}

func TestClientActivateUsesExactInternalRouteAndCredential(t *testing.T) {
	fileID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/files/"+fileID.String()+"/activate" {
			t.Errorf("unexpected activation request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("X-Internal-Token"); got != "dialog-filestorage-test-token" {
			t.Errorf("internal token=%q", got)
			http.Error(w, "missing internal token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := (Client{BaseURL: server.URL, InternalToken: "dialog-filestorage-test-token", HTTPClient: server.Client()}).Activate(context.Background(), fileID); err != nil {
		t.Fatalf("activate file: %v", err)
	}
}

func TestClientSignedGETURLUsesExactInternalContract(t *testing.T) {
	fileID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/v1/files/"+fileID.String()+"/signed-url" {
			t.Errorf("unexpected signed URL request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("X-Internal-Token"); got != "dialog-filestorage-test-token" {
			t.Errorf("internal token=%q", got)
			http.Error(w, "missing internal token", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type=%q", got)
			http.Error(w, "unexpected content type", http.StatusBadRequest)
			return
		}
		var request struct {
			Purpose        string `json:"purpose"`
			Method         string `json:"method"`
			ExpiresMinutes int    `json:"expires_minutes"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			t.Errorf("decode signed URL request: %v", err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if request.Purpose != "dialog_attachment" || request.Method != http.MethodGet || request.ExpiresMinutes != 4 {
			t.Errorf("signed URL request=%+v", request)
			http.Error(w, "unexpected signed URL request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://filestorage.test/signed-object"}`))
	}))
	defer server.Close()

	url, err := (Client{BaseURL: server.URL, InternalToken: "dialog-filestorage-test-token", HTTPClient: server.Client()}).SignedGETURL(context.Background(), fileID, 4)
	if err != nil {
		t.Fatalf("request signed GET URL: %v", err)
	}
	if url != "https://filestorage.test/signed-object" {
		t.Fatalf("signed URL=%q", url)
	}
}
