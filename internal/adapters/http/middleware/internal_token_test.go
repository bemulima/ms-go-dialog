package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireInternalToken_RequiresExactHeader(t *testing.T) {
	writeError := func(w http.ResponseWriter, _ *http.Request, _ error) { w.WriteHeader(http.StatusUnauthorized) }
	handler := RequireInternalToken("secret", writeError)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		name, token string
		want        int
	}{
		{name: "exact", token: "secret", want: http.StatusNoContent},
		{name: "missing", want: http.StatusUnauthorized},
		{name: "whitespace differs", token: " secret ", want: http.StatusUnauthorized},
		{name: "wrong", token: "other", want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/internal/v1/test", nil)
			request.Header.Set("X-Internal-Token", test.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d", response.Code, test.want)
			}
		})
	}
}
