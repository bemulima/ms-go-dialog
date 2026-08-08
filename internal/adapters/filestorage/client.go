package filestorage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	attachmentuc "github.com/bemulima/ms-go-dialog/internal/usecase/attachment"
	"github.com/google/uuid"
)

const maxResponseBytes = 64 << 10

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c Client) UploadTemporary(ctx context.Context, input attachmentuc.TemporaryFileInput) (attachmentuc.StoredFile, error) {
	baseURL, err := c.apiBaseURL()
	if err != nil {
		return attachmentuc.StoredFile{}, err
	}
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/files/upload", reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return attachmentuc.StoredFile{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	go writeTemporaryMultipart(writer, multipartWriter, input)
	response, err := c.httpClient().Do(request)
	if err != nil {
		return attachmentuc.StoredFile{}, fmt.Errorf("upload temporary file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return attachmentuc.StoredFile{}, responseError("upload temporary file", response)
	}
	var payload struct {
		ID uuid.UUID `json:"id"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return attachmentuc.StoredFile{}, err
	}
	if payload.ID == uuid.Nil {
		return attachmentuc.StoredFile{}, fmt.Errorf("filestorage response has no id")
	}
	return attachmentuc.StoredFile{ID: payload.ID}, nil
}

func writeTemporaryMultipart(pipe *io.PipeWriter, writer *multipart.Writer, input attachmentuc.TemporaryFileInput) {
	var writeErr error
	defer func() {
		if closeErr := writer.Close(); writeErr == nil {
			writeErr = closeErr
		}
		_ = pipe.CloseWithError(writeErr)
	}()
	part, err := writer.CreateFormFile("file", input.Filename)
	if err != nil {
		writeErr = err
		return
	}
	if _, err = part.Write(input.Data); err != nil {
		writeErr = err
		return
	}
	fields := []struct{ name, value string }{
		{name: "file_kind", value: "USER_MEDIA"},
		{name: "owner_id", value: input.OwnerID.String()},
		{name: "is_temp", value: "true"},
		{name: "ttl_minutes", value: fmt.Sprintf("%d", input.TTLMinutes)},
	}
	for _, field := range fields {
		if err = writer.WriteField(field.name, field.value); err != nil {
			writeErr = err
			return
		}
	}
}
func (c Client) Activate(ctx context.Context, id uuid.UUID) error {
	return c.noBody(ctx, http.MethodPost, "/files/"+id.String()+"/activate", http.StatusOK, false)
}
func (c Client) Delete(ctx context.Context, id uuid.UUID) error {
	return c.noBody(ctx, http.MethodDelete, "/files/"+id.String(), http.StatusOK, true)
}
func (c Client) SignedGETURL(ctx context.Context, id uuid.UUID, minutes int) (string, error) {
	base, err := c.apiBaseURL()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"purpose": "dialog_attachment", "method": "GET", "expires_minutes": minutes})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/files/"+id.String()+"/signed-url", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient().Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", responseError("request signed URL", response)
	}
	var result struct {
		URL string `json:"url"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&result); err != nil {
		return "", err
	}
	if strings.TrimSpace(result.URL) == "" {
		return "", fmt.Errorf("signed URL is empty")
	}
	return result.URL, nil
}
func (c Client) noBody(ctx context.Context, method, path string, success int, notFoundOK bool) error {
	base, err := c.apiBaseURL()
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == success || (notFoundOK && response.StatusCode == http.StatusNotFound) {
		return nil
	}
	return responseError("filestorage request", response)
}
func (c Client) apiBaseURL() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("filestorage base URL is empty")
	}
	if !strings.HasSuffix(base, "/api/v1") {
		base += "/api/v1"
	}
	return base, nil
}
func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}
func responseError(operation string, response *http.Response) error {
	payload, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	return fmt.Errorf("%s failed with status %d: %s", operation, response.StatusCode, strings.TrimSpace(string(payload)))
}

var _ attachmentuc.FileStorage = (*Client)(nil)
