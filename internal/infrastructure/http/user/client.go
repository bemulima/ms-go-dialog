package user

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bemulima/ms-go-dialog/internal/domain"
	dialoguc "github.com/bemulima/ms-go-dialog/internal/usecase/dialog"
	"github.com/google/uuid"
)

const (
	resolveActiveUsersPath = "/internal/v1/users/active/resolve"
	maxBatchSize           = 1000
	maxResponseBytes       = 64 << 10
)

// Client resolves active dialog participants through the private ms-go-user API.
type Client struct {
	BaseURL       string
	InternalToken string
	HTTPClient    *http.Client
}

type resolveRequest struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

type resolveResponse struct {
	Data struct {
		ActiveUserIDs      []uuid.UUID `json:"active_user_ids"`
		UnavailableUserIDs []uuid.UUID `json:"unavailable_user_ids"`
	} `json:"data"`
}

// RequireActiveUsers returns ErrParticipantUnavailable unless every requested user is active.
func (c Client) RequireActiveUsers(ctx context.Context, ids []uuid.UUID) error {
	requested, err := validateRequestedIDs(ids)
	if err != nil {
		return err
	}
	endpoint, err := c.endpoint()
	if err != nil {
		return dependencyError(err)
	}
	payload, err := json.Marshal(resolveRequest{UserIDs: requested})
	if err != nil {
		return dependencyError(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return dependencyError(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Internal-Token", strings.TrimSpace(c.InternalToken))

	response, err := c.httpClient().Do(request)
	if err != nil {
		return dependencyError(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return dependencyError(fmt.Errorf("unexpected status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return dependencyError(err)
	}
	if len(body) > maxResponseBytes {
		return dependencyError(errors.New("response exceeds size limit"))
	}
	var result resolveResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return dependencyError(err)
	}
	if err = ensureJSONEOF(decoder); err != nil {
		return dependencyError(err)
	}
	if err = validatePartition(requested, result.Data.ActiveUserIDs, result.Data.UnavailableUserIDs); err != nil {
		return dependencyError(err)
	}
	if len(result.Data.UnavailableUserIDs) > 0 {
		return domain.ErrParticipantUnavailable
	}
	return nil
}

func validateRequestedIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 || len(ids) > maxBatchSize {
		return nil, fmt.Errorf("%w: participant count must be between 1 and %d", domain.ErrValidation, maxBatchSize)
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	result := append([]uuid.UUID(nil), ids...)
	for _, id := range result {
		if id == uuid.Nil {
			return nil, fmt.Errorf("%w: participant id is required", domain.ErrValidation)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("%w: participant ids must be unique", domain.ErrValidation)
		}
		seen[id] = struct{}{}
	}
	return result, nil
}

func validatePartition(requested, active, unavailable []uuid.UUID) error {
	wanted := make(map[uuid.UUID]struct{}, len(requested))
	for _, id := range requested {
		wanted[id] = struct{}{}
	}
	resolved := make(map[uuid.UUID]struct{}, len(requested))
	for _, group := range [][]uuid.UUID{active, unavailable} {
		for _, id := range group {
			if id == uuid.Nil {
				return errors.New("response contains an empty user id")
			}
			if _, ok := wanted[id]; !ok {
				return errors.New("response contains an unknown user id")
			}
			if _, duplicate := resolved[id]; duplicate {
				return errors.New("response contains a duplicate user id")
			}
			resolved[id] = struct{}{}
		}
	}
	if len(resolved) != len(wanted) {
		return errors.New("response does not cover all requested user ids")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("response contains multiple JSON values")
		}
		return err
	}
	return nil
}

func (c Client) endpoint() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" || strings.TrimSpace(c.InternalToken) == "" {
		return "", errors.New("user service configuration is incomplete")
	}
	parsed, err := url.ParseRequestURI(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("user service base URL is invalid")
	}
	return base + resolveActiveUsersPath, nil
}

func (c Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 3 * time.Second}
}

func dependencyError(cause error) error {
	return fmt.Errorf("%w: user service request failed: %v", domain.ErrDependencyUnavailable, cause)
}

var _ dialoguc.ParticipantResolver = (*Client)(nil)
