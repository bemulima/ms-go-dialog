package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bemulima/ms-go-dialog/internal/domain"
)

func TestClassifyParticipantAndDependencyErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "participant", err: fmtWrapped(domain.ErrParticipantUnavailable), wantStatus: http.StatusUnprocessableEntity, wantCode: "participant_unavailable"},
		{name: "dependency", err: fmtWrapped(domain.ErrDependencyUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: "dependency_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, code, _ := classifyError(test.err)
			if status != test.wantStatus || code != test.wantCode {
				t.Fatalf("classification=(%d, %q), want (%d, %q)", status, code, test.wantStatus, test.wantCode)
			}
		})
	}
}

func fmtWrapped(err error) error {
	return errors.Join(errors.New("context"), err)
}
